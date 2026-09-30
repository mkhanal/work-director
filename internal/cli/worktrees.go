package cli

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"wd/internal/core"
	"wd/internal/ledger"
	"wd/internal/project"
)

// The director's worktrees: it makes them for epics and their tasks under
// $WD_HOME/worktrees, and removes them when the work is done and removing
// them loses nothing. A worktree it did not make is never touched.

func sharedBranch(goal core.Work) string { return "wd-" + goal.ID }

func branchFor(w core.Work) string { return "wd-" + w.ID }

// ensureSharedWorktree creates or reuses the goal's one shared worktree on
// branch wd-<goal>, registered in the ledger.
func (c *Cli) ensureSharedWorktree(goal core.Work, p *project.Project) (string, error) {
	wts, err := c.Ledger.Worktrees(goal.ID)
	if err != nil {
		return "", err
	}
	for _, wt := range wts {
		if wt.Kind == core.WorktreeShared && wt.State == core.WorktreeActive {
			return wt.Path, nil
		}
	}
	if _, err := c.runGit([]string{"worktree", "prune"}, p.Path); err != nil {
		return "", err
	}
	path := filepath.Join(c.Worktrees, p.Name+"-goal-"+goal.ID)
	r, err := c.runGit([]string{"worktree", "add", "-b", sharedBranch(goal), path}, p.Path)
	if err != nil {
		return "", err
	}
	if r.Code != 0 {
		r, err = c.runGit([]string{"worktree", "add", path, sharedBranch(goal)}, p.Path)
		if err != nil {
			return "", err
		}
	}
	if r.Code != 0 {
		return "", fail("cannot create shared worktree at %s: %s", path, strings.TrimSpace(r.Stderr))
	}
	wt, err := c.Ledger.AddWorktree(goal.ID, ledger.WorktreeInfo{
		Path:   path,
		Branch: ptr(sharedBranch(goal)),
		Kind:   core.WorktreeShared,
		Origin: core.OriginDirector,
	})
	if err != nil {
		return "", err
	}
	return wt.Path, nil
}

// ensurePrivateWorktree creates a private worktree for a task, branched off
// the goal's shared branch.
func (c *Cli) ensurePrivateWorktree(w core.Work, goal core.Work, p *project.Project) (string, error) {
	wts, err := c.Ledger.Worktrees(w.ID)
	if err != nil {
		return "", err
	}
	for _, wt := range wts {
		if wt.Kind == core.WorktreePrivate && wt.State == core.WorktreeActive {
			return wt.Path, nil
		}
	}
	if _, err := c.runGit([]string{"worktree", "prune"}, p.Path); err != nil {
		return "", err
	}
	path := filepath.Join(c.Worktrees, p.Name+"-"+w.ID)
	r, err := c.runGit([]string{"worktree", "add", "-b", branchFor(w), path, sharedBranch(goal)}, p.Path)
	if err != nil {
		return "", err
	}
	if r.Code != 0 {
		return "", fail("cannot create private worktree at %s: %s", path, strings.TrimSpace(r.Stderr))
	}
	wt, err := c.Ledger.AddWorktree(w.ID, ledger.WorktreeInfo{
		Path:   path,
		Branch: ptr(branchFor(w)),
		Kind:   core.WorktreePrivate,
		Origin: core.OriginDirector,
	})
	if err != nil {
		return "", err
	}
	return wt.Path, nil
}

// keptWorktree is a director-made worktree that stayed, and why.
type keptWorktree struct {
	Worktree core.Worktree `json:"worktree"`
	Reasons  []string      `json:"reasons"`
}

func (k keptWorktree) String() string {
	return fmt.Sprintf("kept worktree %s: %s", k.Worktree.Path, strings.Join(k.Reasons, "; "))
}

// releaseWorktrees removes each director-made worktree of done work w still
// on disk, with its branch, when that loses nothing: the worktree is
// unlocked, holds no local changes, and its branch's content is already in
// the ref it lands on. Every other worktree stays untouched and is returned
// with its reasons.
func (c *Cli) releaseWorktrees(w core.Work) (removed []core.Worktree, kept []keptWorktree, err error) {
	removed = []core.Worktree{}
	wts, err := c.Ledger.Worktrees(w.ID)
	if err != nil {
		return nil, nil, err
	}
	for _, wt := range wts {
		if wt.Origin != core.OriginDirector || wt.State == core.WorktreeRemoved {
			continue
		}
		p, err := c.project(w.Project)
		if err != nil {
			return nil, nil, err
		}
		reasons, err := c.keepReasons(w, wt, p)
		if err != nil {
			return nil, nil, err
		}
		if len(reasons) > 0 {
			kept = append(kept, keptWorktree{Worktree: wt, Reasons: reasons})
			continue
		}
		after, err := c.removeWorktree(w, wt, p)
		if err != nil {
			return nil, nil, err
		}
		removed = append(removed, after)
	}
	return removed, kept, nil
}

// keepReasons says why removing wt would lose something; none means it
// loses nothing.
func (c *Cli) keepReasons(w core.Work, wt core.Worktree, p *project.Project) ([]string, error) {
	if wt.Branch == nil {
		return []string{"it records no branch"}, nil
	}
	var reasons []string
	locked, err := c.lockReason(wt, p)
	if err != nil {
		return nil, err
	}
	if locked != "" {
		reasons = append(reasons, locked)
	}
	status, err := c.runGit([]string{"-C", wt.Path, "status", "--porcelain"}, p.Path)
	if err != nil {
		return nil, err
	}
	if status.Code != 0 {
		reasons = append(reasons, "its status cannot be read: "+strings.TrimSpace(status.Stderr))
	} else if strings.TrimSpace(status.Stdout) != "" {
		reasons = append(reasons, "it has local changes")
	}
	onto, unreachable, err := c.landingRef(w, wt, p)
	if err != nil {
		return nil, err
	}
	if unreachable != "" {
		return append(reasons, unreachable), nil
	}
	unlanded, err := c.unlanded(*wt.Branch, onto, p)
	if err != nil {
		return nil, err
	}
	if unlanded != "" {
		reasons = append(reasons, unlanded)
	}
	return reasons, nil
}

// removeWorktree removes wt from disk, records that, then deletes its branch.
func (c *Cli) removeWorktree(w core.Work, wt core.Worktree, p *project.Project) (core.Worktree, error) {
	r, err := c.runGit([]string{"worktree", "remove", wt.Path}, p.Path)
	if err != nil {
		return core.Worktree{}, err
	}
	if r.Code != 0 {
		return core.Worktree{}, fail("cannot remove worktree %s: %s", wt.Path, strings.TrimSpace(r.Stderr))
	}
	after, err := c.Ledger.SetWorktreeState(wt.ID, core.WorktreeRemoved)
	if err != nil {
		return core.Worktree{}, err
	}
	if _, err := c.Ledger.AddEvent(w.ID, core.EventNote, fmt.Sprintf("removed worktree %s and branch %s", wt.Path, *wt.Branch)); err != nil {
		return core.Worktree{}, err
	}
	r, err = c.runGit([]string{"branch", "-D", *wt.Branch}, p.Path)
	if err != nil {
		return core.Worktree{}, err
	}
	if r.Code != 0 {
		return core.Worktree{}, fail("removed worktree %s but not its branch %s: %s", wt.Path, *wt.Branch, strings.TrimSpace(r.Stderr))
	}
	return after, nil
}

// lockReason is why wt stays because it is locked, or "" when it is not. A
// lock is its owner saying the worktree must not be pruned; wd never locks
// its own, so it never overrides one.
func (c *Cli) lockReason(wt core.Worktree, p *project.Project) (string, error) {
	r, err := c.runGit([]string{"-C", wt.Path, "rev-parse", "--path-format=absolute", "--git-path", "locked"}, p.Path)
	if err != nil {
		return "", err
	}
	if r.Code != 0 {
		return "", nil
	}
	body, err := os.ReadFile(strings.TrimSpace(r.Stdout))
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if why := strings.TrimSpace(string(body)); why != "" {
		return "it is locked: " + why, nil
	}
	return "it is locked", nil
}

// landingRef is the ref wt's branch lands on: the goal's shared branch for a
// task's private worktree, the project's default branch for a goal's shared
// one. A default branch with an upstream lands on the upstream, fetched
// first, because a pull request merges there. unreachable says why there is
// no ref to compare with.
func (c *Cli) landingRef(w core.Work, wt core.Worktree, p *project.Project) (ref, unreachable string, err error) {
	if wt.Kind == core.WorktreePrivate {
		if w.Parent == nil {
			return "", "it is a private worktree of work with no goal", nil
		}
		goal, err := c.Ledger.Get(*w.Parent)
		if err != nil {
			return "", "", err
		}
		return sharedBranch(goal), "", nil
	}
	upstream, err := c.runGit([]string{"rev-parse", "--abbrev-ref", "--symbolic-full-name", p.DefaultBranch + "@{upstream}"}, p.Path)
	if err != nil {
		return "", "", err
	}
	if upstream.Code != 0 {
		return p.DefaultBranch, "", nil
	}
	remote, err := c.gitConfig("branch."+p.DefaultBranch+".remote", p)
	if err != nil {
		return "", "", err
	}
	merge, err := c.gitConfig("branch."+p.DefaultBranch+".merge", p)
	if err != nil {
		return "", "", err
	}
	ref = strings.TrimSpace(upstream.Stdout)
	fetch, err := c.runGit([]string{"fetch", "--quiet", remote, merge}, p.Path)
	if err != nil {
		return "", "", err
	}
	if fetch.Code != 0 {
		return "", fmt.Sprintf("fetching %s failed: %s", ref, strings.TrimSpace(fetch.Stderr)), nil
	}
	return ref, "", nil
}

func (c *Cli) gitConfig(key string, p *project.Project) (string, error) {
	r, err := c.runGit([]string{"config", "--get", key}, p.Path)
	if err != nil {
		return "", err
	}
	if r.Code != 0 {
		return "", fail("git config %s is unset in %s", key, p.Path)
	}
	return strings.TrimSpace(r.Stdout), nil
}

// unlanded is "" when merging branch into onto would change nothing, so
// every change on branch is already in onto, whether merged, rebased or
// squashed; otherwise it says that branch still holds work.
func (c *Cli) unlanded(branch, onto string, p *project.Project) (string, error) {
	merged, err := c.runGit([]string{"merge-tree", "--write-tree", onto, branch}, p.Path)
	if err != nil {
		return "", err
	}
	switch merged.Code {
	case 0:
	case 1:
		return fmt.Sprintf("branch %s conflicts with %s, so it holds work not landed there", branch, onto), nil
	default:
		return fmt.Sprintf("branch %s cannot be compared with %s: %s", branch, onto, strings.TrimSpace(merged.Stderr)), nil
	}
	tree, err := c.runGit([]string{"rev-parse", onto + "^{tree}"}, p.Path)
	if err != nil {
		return "", err
	}
	if tree.Code != 0 {
		return "", fail("cannot read the tree of %s: %s", onto, strings.TrimSpace(tree.Stderr))
	}
	result, _, _ := strings.Cut(merged.Stdout, "\n")
	if strings.TrimSpace(result) != strings.TrimSpace(tree.Stdout) {
		return fmt.Sprintf("branch %s holds work not landed in %s", branch, onto), nil
	}
	return "", nil
}
