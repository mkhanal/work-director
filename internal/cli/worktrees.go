package cli

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"wd/internal/coordinator"
	"wd/internal/core"
	"wd/internal/ledger"
	"wd/internal/project"
	"wd/internal/runner"
)

// The worktrees recorded on work: wd makes them for goals and their tasks
// under $WD_HOME/worktrees, and an executor may attach its own. Once the work
// is finished each is released: its directory goes when that loses nothing on
// disk, and its branch when its content has landed. A worktree no work
// records is never touched.

func sharedBranch(goal core.Work) string { return "wd-" + goal.ID }

func branchFor(w core.Work) string { return "wd-" + w.ID }

// ensureSharedWorktree returns the goal's one shared worktree on branch
// wd-<goal>, making it when the goal has none on disk. A new branch starts
// where the goal lands, never at whatever the project's checkout has out.
func (c *Cli) ensureSharedWorktree(goal core.Work, p *project.Project) (string, error) {
	path, ok, err := c.activeWorktree(goal, core.WorktreeShared, p)
	if err != nil || ok {
		return path, err
	}
	if path == "" {
		path = filepath.Join(c.Worktrees, p.Name+"-goal-"+goal.ID)
	}
	onto, _, err := c.mainRef(p)
	if err != nil {
		return "", err
	}
	tree, err := c.Ledger.Under(goal.ID)
	if err != nil {
		return "", err
	}
	owners := make([]string, len(tree))
	for i, w := range tree {
		owners[i] = branchFor(w)
	}
	return c.makeWorktree(goal, path, sharedBranch(goal), onto, owners, core.WorktreeShared, p)
}

// ensurePrivateWorktree returns task w's private worktree, making it when w
// has none on disk on a branch started at its top goal's shared branch, which
// is made first when the goal has none.
func (c *Cli) ensurePrivateWorktree(w core.Work, p *project.Project) (string, error) {
	path, ok, err := c.activeWorktree(w, core.WorktreePrivate, p)
	if err != nil || ok {
		return path, err
	}
	if path == "" {
		path = filepath.Join(c.Worktrees, p.Name+"-"+w.ID)
	}
	top, err := c.topGoal(w)
	if err != nil {
		return "", err
	}
	if _, err := c.ensureSharedWorktree(top, p); err != nil {
		return "", err
	}
	return c.makeWorktree(w, path, branchFor(w), sharedBranch(top), []string{branchFor(w)}, core.WorktreePrivate, p)
}

// activeWorktree is w's active worktree of kind, ok when it is on disk.
// Otherwise it is the path w's last worktree of kind had, if any, to be made
// again at; one recorded active but gone from disk is recorded as gone.
func (c *Cli) activeWorktree(w core.Work, kind core.WorktreeKind, p *project.Project) (string, bool, error) {
	wts, err := c.Ledger.Worktrees(w.ID)
	if err != nil {
		return "", false, err
	}
	last := ""
	for _, wt := range wts {
		if wt.Kind != kind {
			continue
		}
		last = wt.Path
		if wt.State != core.WorktreeActive {
			continue
		}
		there, err := c.present(wt, p)
		if err != nil || there {
			return wt.Path, there, err
		}
		if err := c.recordGone(wt); err != nil {
			return "", false, err
		}
	}
	return last, false, nil
}

// reclaimWorktrees makes again, at their paths, the worktrees wd made for w
// that were released or went from disk, now that w works again.
func (c *Cli) reclaimWorktrees(w core.Work) error {
	wts, err := c.Ledger.Worktrees(w.ID)
	if err != nil {
		return err
	}
	last := map[core.WorktreeKind]core.Worktree{}
	for _, wt := range wts {
		if wt.Origin == core.OriginDirector {
			last[wt.Kind] = wt
		}
	}
	if len(last) == 0 {
		return nil
	}
	p, err := c.project(w.Project)
	if err != nil {
		return err
	}
	if wt, ok := last[core.WorktreeShared]; ok && (wt.State == core.WorktreeRemoved || wt.State == core.WorktreeGone) {
		if _, err := c.ensureSharedWorktree(w, p); err != nil {
			return err
		}
	}
	if wt, ok := last[core.WorktreePrivate]; ok && (wt.State == core.WorktreeRemoved || wt.State == core.WorktreeGone) {
		if _, err := c.ensurePrivateWorktree(w, p); err != nil {
			return err
		}
	}
	return nil
}

// makeWorktree makes a worktree for w at path on branch and records it. A
// branch that does not exist starts at onto; one that exists is reused, after
// starting again at onto when it holds no commits of its own (owners are the
// branches whose commits count as its own).
func (c *Cli) makeWorktree(w core.Work, path, branch, onto string, owners []string, kind core.WorktreeKind, p *project.Project) (string, error) {
	if _, err := c.gitOK(p.Path, "worktree", "prune"); err != nil {
		return "", err
	}
	exists, err := c.branchExists(branch, p)
	if err != nil {
		return "", err
	}
	args := []string{"worktree", "add", "-b", branch, path, onto}
	if exists {
		own, err := c.ownCommits(branch, onto, owners, p)
		if err != nil {
			return "", err
		}
		if own == 0 {
			if _, err := c.gitOK(p.Path, "branch", "-f", branch, onto); err != nil {
				return "", err
			}
			if _, err := c.Ledger.AddEvent(w.ID, core.EventNote, fmt.Sprintf("started branch %s again at %s: it held no commits of its own", branch, onto)); err != nil {
				return "", err
			}
		}
		args = []string{"worktree", "add", path, branch}
	}
	r, err := c.runGit(args, p.Path)
	if err != nil {
		return "", err
	}
	if r.Code != 0 {
		return "", fail("cannot create a worktree at %s: %s", path, strings.TrimSpace(r.Stderr))
	}
	if err := c.installIn(path, p); err != nil {
		return "", err
	}
	wt, err := c.Ledger.AddWorktree(w.ID, ledger.WorktreeInfo{
		Path:   path,
		Branch: ptr(branch),
		Kind:   kind,
		Origin: core.OriginDirector,
	})
	if err != nil {
		return "", err
	}
	return wt.Path, nil
}

// installTail bounds how much of a failed install's output is shown.
const installTail = 2000

// installIn installs the dependencies the repository's lockfile pins in a
// worktree just made at path, so nothing run there reads another checkout's.
// A worktree whose install fails is removed, so the next start makes it again.
func (c *Cli) installIn(path string, p *project.Project) error {
	kit, err := project.ReadToolkit(path)
	if err != nil || kit.Install == "" {
		return err
	}
	r, err := runner.Run([]string{"bash", "-lc", kit.Install}, path)
	if err == nil && r.Code == 0 {
		return nil
	}
	said := ""
	if err != nil {
		said = err.Error()
	} else {
		said = strings.TrimSpace(r.Stdout + "\n" + r.Stderr)
		if n := len(said); n > installTail {
			said = "…" + said[n-installTail:]
		}
	}
	if rm, rerr := c.runGit([]string{"worktree", "remove", "--force", path}, p.Path); rerr != nil {
		return rerr
	} else if rm.Code != 0 {
		return fail("installing dependencies in %s failed (`%s`: %s), and the worktree could not be removed: %s", path, kit.Install, said, strings.TrimSpace(rm.Stderr))
	}
	return fail("installing dependencies in %s failed, so the worktree was removed: `%s`: %s", path, kit.Install, said)
}

// gitOK runs git in dir and returns what it printed, failing with git's own
// words when it exits non-zero.
func (c *Cli) gitOK(dir string, args ...string) (string, error) {
	r, err := c.runGit(args, dir)
	if err != nil {
		return "", err
	}
	if r.Code != 0 {
		return "", fail("git %s in %s: %s", strings.Join(args, " "), dir, strings.TrimSpace(r.Stderr))
	}
	return strings.TrimSpace(r.Stdout), nil
}

func (c *Cli) branchExists(branch string, p *project.Project) (bool, error) {
	r, err := c.runGit([]string{"rev-parse", "--verify", "--quiet", "refs/heads/" + branch}, p.Path)
	if err != nil {
		return false, err
	}
	return r.Code == 0, nil
}

// ownCommits counts the commits on branch that are past onto and held by no
// branch, remote branch or tag but owners. A branch made from a checkout that
// had another branch out carries that branch's commits; those are not its own.
func (c *Cli) ownCommits(branch, onto string, owners []string, p *project.Project) (int, error) {
	args := []string{"rev-list", "--count", branch, "--not", onto}
	for _, o := range owners {
		args = append(args, "--exclude="+o)
	}
	args = append(args, "--branches")
	for _, o := range owners {
		args = append(args, "--exclude=*/"+o)
	}
	args = append(args, "--remotes", "--tags")
	out, err := c.gitOK(p.Path, args...)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(out)
}

// present reports whether wt is still on disk. A directory gone that git
// still lists, such as a locked worktree on a drive not mounted, counts as
// present: it is not wd's to forget.
func (c *Cli) present(wt core.Worktree, p *project.Project) (bool, error) {
	if _, err := os.Stat(wt.Path); err == nil {
		return true, nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return false, err
	}
	if _, err := c.gitOK(p.Path, "worktree", "prune"); err != nil {
		return false, err
	}
	listed, err := c.gitOK(p.Path, "worktree", "list", "--porcelain")
	if err != nil {
		return false, err
	}
	want, err := canonical(wt.Path)
	if err != nil {
		return false, err
	}
	for _, line := range strings.Split(listed, "\n") {
		if path, ok := strings.CutPrefix(line, "worktree "); ok {
			got, err := canonical(path)
			if err != nil {
				return false, err
			}
			if got == want {
				return true, nil
			}
		}
	}
	return false, nil
}

// canonical is path with symlinks in its deepest existing ancestor resolved,
// so a path git prints compares with the one wd recorded.
func canonical(path string) (string, error) {
	path = filepath.Clean(path)
	rest := ""
	for dir := path; ; dir = filepath.Dir(dir) {
		real, err := filepath.EvalSymlinks(dir)
		if err == nil {
			return filepath.Join(real, rest), nil
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return "", err
		}
		if filepath.Dir(dir) == dir {
			return path, nil
		}
		rest = filepath.Join(filepath.Base(dir), rest)
	}
}

func (c *Cli) recordGone(wt core.Worktree) error {
	if _, err := c.Ledger.SetWorktreeState(wt.ID, core.WorktreeGone); err != nil {
		return err
	}
	_, err := c.Ledger.AddEvent(wt.Work, core.EventNote, fmt.Sprintf("the worktree at %s is gone from disk", wt.Path))
	return err
}

// keptWorktree is a recorded worktree whose directory stayed, and why.
type keptWorktree struct {
	Worktree core.Worktree `json:"worktree"`
	Reasons  []string      `json:"reasons"`
	// Person is whether only a person can clear the reasons: a lock or local
	// changes, not a session that will stop.
	Person bool `json:"person"`
}

func (k keptWorktree) String() string {
	return fmt.Sprintf("kept worktree %s: %s", k.Worktree.Path, strings.Join(k.Reasons, "; "))
}

// keptBranch is a released worktree's branch that stayed, and why.
type keptBranch struct {
	Worktree core.Worktree `json:"worktree"`
	Branch   string        `json:"branch"`
	Reason   string        `json:"reason"`
}

func (k keptBranch) String() string {
	return fmt.Sprintf("kept branch %s: %s", k.Branch, k.Reason)
}

// released is what releasing worktrees did, or would do on a dry run.
type released struct {
	Removed  []core.Worktree `json:"removed"`
	Kept     []keptWorktree  `json:"kept"`
	Branches []keptBranch    `json:"branches_kept"`
	Gone     []core.Worktree `json:"gone"`
	// Failed says, per work, why its worktrees could not be released.
	Failed []string `json:"failed"`
}

func newReleased() released {
	return released{Removed: []core.Worktree{}, Kept: []keptWorktree{}, Branches: []keptBranch{}, Gone: []core.Worktree{}, Failed: []string{}}
}

func (r *released) add(o released) {
	r.Removed = append(r.Removed, o.Removed...)
	r.Kept = append(r.Kept, o.Kept...)
	r.Branches = append(r.Branches, o.Branches...)
	r.Gone = append(r.Gone, o.Gone...)
	r.Failed = append(r.Failed, o.Failed...)
}

// lines says r for a person, one line per worktree or branch.
func (r released) lines(dry bool) []string {
	removed, gone := "removed worktree ", "recorded as gone: the worktree at "
	if dry {
		removed, gone = "would remove worktree ", "would record as gone: the worktree at "
	}
	var out []string
	for _, wt := range r.Removed {
		out = append(out, removed+wt.Path)
	}
	for _, k := range r.Kept {
		out = append(out, k.String())
	}
	for _, b := range r.Branches {
		out = append(out, b.String())
	}
	for _, wt := range r.Gone {
		out = append(out, gone+wt.Path+" is no longer on disk; it is made again from its branch when needed")
	}
	return append(out, r.Failed...)
}

// releaseWorktrees releases each worktree recorded on finished work w: its
// directory is removed when it is unlocked, holds no local changes and no
// agent works in it, and its branch is deleted when its content is already
// where it lands. A worktree only a person can clear raises one concern. A
// dry run changes nothing.
func (c *Cli) releaseWorktrees(w core.Work, dry bool) (released, error) {
	out := newReleased()
	wts, err := c.Ledger.Worktrees(w.ID)
	if err != nil {
		return out, err
	}
	for _, wt := range wts {
		if wt.State == core.WorktreeRemoved {
			continue
		}
		p, err := c.project(w.Project)
		if err != nil {
			return out, err
		}
		if wt.State != core.WorktreeGone {
			there, err := c.present(wt, p)
			if err != nil {
				return out, err
			}
			if there {
				kept, err := c.keepReasons(wt, p)
				if err != nil {
					return out, err
				}
				if len(kept.Reasons) > 0 {
					out.Kept = append(out.Kept, kept)
					if kept.Person && !dry {
						if err := c.raiseKept(w, kept); err != nil {
							return out, err
						}
					}
					continue
				}
				if !dry {
					if _, err := c.gitOK(p.Path, "worktree", "remove", wt.Path); err != nil {
						return out, err
					}
				}
			}
		}
		branch, err := c.releaseBranch(w, wt, p, dry)
		if err != nil {
			return out, err
		}
		if branch != nil {
			out.Branches = append(out.Branches, *branch)
		}
		if !dry {
			if wt, err = c.Ledger.SetWorktreeState(wt.ID, core.WorktreeRemoved); err != nil {
				return out, err
			}
			body := fmt.Sprintf("released worktree %s", wt.Path)
			if branch != nil {
				body += "; " + branch.String()
			}
			if _, err := c.Ledger.AddEvent(w.ID, core.EventNote, body); err != nil {
				return out, err
			}
		}
		out.Removed = append(out.Removed, wt)
	}
	return out, nil
}

// raiseKept raises the concern that k stays until a person clears it, unless
// w already holds it open.
func (c *Cli) raiseKept(w core.Work, k keptWorktree) error {
	text := fmt.Sprintf("%s. Run wd worktree remove %s once that is resolved.", k, w.ID)
	open, err := c.Ledger.Concerns(&w.ID)
	if err != nil {
		return err
	}
	for _, cx := range open {
		if cx.Resolved == 0 && cx.Text == text {
			return nil
		}
	}
	_, err = c.Ledger.AddConcern(w.ID, text)
	return err
}

// releaseBranch deletes wt's branch when its content is already where it
// lands, and otherwise returns it kept with why. A branch already gone, or
// none recorded, is nothing to release.
func (c *Cli) releaseBranch(w core.Work, wt core.Worktree, p *project.Project, dry bool) (*keptBranch, error) {
	if wt.Branch == nil {
		return nil, nil
	}
	if exists, err := c.branchExists(*wt.Branch, p); err != nil || !exists {
		return nil, err
	}
	keep := func(why string) (*keptBranch, error) {
		return &keptBranch{Worktree: wt, Branch: *wt.Branch, Reason: why}, nil
	}
	onto, unreachable, err := c.landingRef(w, wt, p)
	if err != nil {
		return nil, err
	}
	if unreachable != "" {
		return keep(unreachable)
	}
	unlanded, err := c.unlanded(*wt.Branch, onto, p)
	if err != nil {
		return nil, err
	}
	if unlanded != "" {
		return keep(unlanded)
	}
	if dry {
		return nil, nil
	}
	r, err := c.runGit([]string{"branch", "-D", *wt.Branch}, p.Path)
	if err != nil {
		return nil, err
	}
	if r.Code != 0 {
		return keep("it could not be deleted: " + strings.TrimSpace(r.Stderr))
	}
	return nil, nil
}

// keepReasons says why removing wt's directory would lose something; none
// means it loses nothing.
func (c *Cli) keepReasons(wt core.Worktree, p *project.Project) (keptWorktree, error) {
	k := keptWorktree{Worktree: wt}
	same, err := samePath(wt.Path, p.Path)
	if err != nil {
		return k, err
	}
	if same {
		k.Reasons, k.Person = []string{"it is the project's own checkout"}, true
		return k, nil
	}
	locked, err := c.lockReason(wt, p)
	if err != nil {
		return k, err
	}
	if locked != "" {
		k.Reasons, k.Person = append(k.Reasons, locked), true
	}
	status, err := c.runGit([]string{"-C", wt.Path, "status", "--porcelain"}, p.Path)
	if err != nil {
		return k, err
	}
	if status.Code != 0 {
		k.Reasons, k.Person = append(k.Reasons, "its status cannot be read: "+strings.TrimSpace(status.Stderr)), true
	} else if strings.TrimSpace(status.Stdout) != "" {
		k.Reasons, k.Person = append(k.Reasons, "it has local changes"), true
	}
	working, err := c.workingIn(wt.Path, p)
	if err != nil {
		return k, err
	}
	if working != "" {
		k.Reasons = append(k.Reasons, working)
	}
	return k, nil
}

func samePath(a, b string) (bool, error) {
	ca, err := canonical(a)
	if err != nil {
		return false, err
	}
	cb, err := canonical(b)
	return ca == cb, err
}

// workingIn says which agent session of the project is working in dir or
// waiting there on a person, or "" when none is. An idle session is not
// working: it does nothing until sent to.
func (c *Cli) workingIn(dir string, p *project.Project) (string, error) {
	root, err := canonical(dir)
	if err != nil {
		return "", err
	}
	works, err := c.Ledger.List(ledger.ListFilter{Project: &p.Name})
	if err != nil {
		return "", err
	}
	for _, w := range works {
		var handles []runner.Handle
		if h, ok, err := coordinator.Handle(c.Ledger, w, p); err != nil {
			return "", err
		} else if ok {
			handles = append(handles, h)
		}
		if core.IsGoal(w.Kind) {
			if h, ok, err := coordinator.Supervisor(c.Ledger, w, p); err != nil {
				return "", err
			} else if ok {
				handles = append(handles, h)
			}
			chores, err := c.chores(w, p)
			if err != nil {
				return "", err
			}
			for _, ch := range chores {
				if ch.finished == nil {
					handles = append(handles, ch.handle)
				}
			}
		}
		for _, h := range handles {
			cwd, err := canonical(h.Cwd)
			if err != nil {
				return "", err
			}
			if cwd != root && !strings.HasPrefix(cwd, root+string(filepath.Separator)) {
				continue
			}
			r, err := runner.RunnerNamed(h.Runner)
			if err != nil {
				return "", err
			}
			status, err := r.Status(h)
			if err != nil {
				return fmt.Sprintf("whether %s's agent session still works in it cannot be read: %v", w.ID, err), nil
			}
			if status == runner.StatusRunning || status == runner.StatusWaiting {
				return fmt.Sprintf("%s's agent session is still working in it", w.ID), nil
			}
		}
	}
	return "", nil
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

// landingRef is the ref wt's branch lands on: its top goal's shared branch
// for a task's private worktree while that branch exists, else the project's
// main ref. unreachable says why there is no ref to compare with.
func (c *Cli) landingRef(w core.Work, wt core.Worktree, p *project.Project) (ref, unreachable string, err error) {
	if wt.Kind == core.WorktreePrivate && w.Parent != nil {
		top, err := c.topGoal(w)
		if err != nil {
			return "", "", err
		}
		exists, err := c.branchExists(sharedBranch(top), p)
		if err != nil {
			return "", "", err
		}
		if exists {
			return sharedBranch(top), "", nil
		}
	}
	ref, fetchFailed, err := c.mainRef(p)
	if err != nil || fetchFailed != "" {
		return "", fetchFailed, err
	}
	return ref, "", nil
}

// mainRef is where a goal lands: the project's default branch, or its
// upstream fetched first, because a pull request merges there. fetchFailed
// says why the upstream could not be fetched; ref is then as last fetched.
func (c *Cli) mainRef(p *project.Project) (ref, fetchFailed string, err error) {
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
		return ref, fmt.Sprintf("fetching %s failed: %s", ref, strings.TrimSpace(fetch.Stderr)), nil
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
		return fmt.Sprintf("it conflicts with %s, so it holds work not landed there", onto), nil
	default:
		return fmt.Sprintf("it cannot be compared with %s: %s", onto, strings.TrimSpace(merged.Stderr)), nil
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
		return fmt.Sprintf("it holds work not landed in %s", onto), nil
	}
	return "", nil
}

// releaseFinished releases the worktrees of each of ws that is finished,
// leaving what it did on the work's record.
func (c *Cli) releaseFinished(ws ...core.Work) error {
	for _, w := range ws {
		if !core.AtRest(w.State) {
			continue
		}
		if _, err := c.releaseWorktrees(w, false); err != nil {
			return err
		}
	}
	return nil
}

// cleanWorktrees releases the worktrees of every finished work, and records
// as gone each of unfinished work whose directory went from disk. One work's
// worktrees that cannot be released are said and the rest still are.
func (c *Cli) cleanWorktrees(dry bool) (released, error) {
	out := newReleased()
	wts, err := c.Ledger.UnreleasedWorktrees()
	if err != nil {
		return out, err
	}
	seen := map[string]bool{}
	for _, wt := range wts {
		w, err := c.Ledger.Get(wt.Work)
		if err != nil {
			return out, err
		}
		if core.AtRest(w.State) {
			if seen[w.ID] {
				continue
			}
			seen[w.ID] = true
			r, err := c.releaseWorktrees(w, dry)
			if err != nil {
				out.Failed = append(out.Failed, fmt.Sprintf("could not release the worktrees of %s: %v", w.ID, err))
			}
			out.add(r)
			continue
		}
		if wt.State != core.WorktreeActive && wt.State != core.WorktreeMerged {
			continue
		}
		p, err := c.project(w.Project)
		if err != nil {
			out.Failed = append(out.Failed, fmt.Sprintf("could not check the worktree at %s: %v", wt.Path, err))
			continue
		}
		there, err := c.present(wt, p)
		if err != nil {
			out.Failed = append(out.Failed, fmt.Sprintf("could not check the worktree at %s: %v", wt.Path, err))
			continue
		}
		if there {
			continue
		}
		if !dry {
			if err := c.recordGone(wt); err != nil {
				return out, err
			}
		}
		out.Gone = append(out.Gone, wt)
	}
	return out, nil
}

// tidyWorktrees is the sweep serve runs now and then.
func (c *Cli) tidyWorktrees() error {
	_, err := c.cleanWorktrees(false)
	return err
}

// syncGoal brings the shared branch of goal's top goal up to date with where
// it lands, in its worktree: a branch holding no commits of its own starts
// again there, any other has it merged in. A conflict is undone and named.
func (c *Cli) syncGoal(goal core.Work, p *project.Project) (string, error) {
	top, err := c.topGoal(goal)
	if err != nil {
		return "", err
	}
	path, err := c.ensureSharedWorktree(top, p)
	if err != nil {
		return "", err
	}
	branch := sharedBranch(top)
	status, err := c.gitOK(path, "status", "--porcelain")
	if err != nil {
		return "", err
	}
	if status != "" {
		return "", fail("the goal's worktree at %s has changes not committed; it is brought up to date once they are committed or put away", path)
	}
	if working, err := c.workingIn(path, p); err != nil {
		return "", err
	} else if working != "" {
		return "", fail("the goal's worktree is not brought up to date while %s", strings.Replace(working, " in it", " there", 1))
	}
	onto, fetchFailed, err := c.mainRef(p)
	if err != nil {
		return "", err
	}
	if fetchFailed != "" {
		return "", fail("the goal's branch was not brought up to date: %s", fetchFailed)
	}
	tree, err := c.Ledger.Under(top.ID)
	if err != nil {
		return "", err
	}
	owners := make([]string, len(tree))
	for i, w := range tree {
		owners[i] = branchFor(w)
	}
	own, err := c.ownCommits(branch, onto, owners, p)
	if err != nil {
		return "", err
	}
	said := ""
	if own == 0 {
		at, err := c.gitOK(path, "rev-parse", branch, onto)
		if err != nil {
			return "", err
		}
		if f := strings.Fields(at); len(f) == 2 && f[0] == f[1] {
			return fmt.Sprintf("%s is already up to date with %s", branch, onto), nil
		}
		for _, w := range tree[1:] {
			if held, ok, err := c.activeWorktree(w, core.WorktreePrivate, p); err != nil {
				return "", err
			} else if ok {
				return "", fail("%s holds no commits of its own and would start again at %s, but %s works in a worktree of its own started from it, at %s; finish or drop that work first", branch, onto, w.ID, held)
			}
		}
		if _, err := c.gitOK(path, "reset", "--quiet", "--hard", onto); err != nil {
			return "", err
		}
		said = fmt.Sprintf("started %s again at %s: it held no commits of its own", branch, onto)
	} else {
		r, err := c.runGit([]string{"merge-base", "--is-ancestor", onto, branch}, path)
		if err != nil {
			return "", err
		}
		if r.Code == 0 {
			return fmt.Sprintf("%s is already up to date with %s", branch, onto), nil
		}
		merged, err := c.runGit([]string{"merge", "--no-edit", onto}, path)
		if err != nil {
			return "", err
		}
		if merged.Code != 0 {
			files, err := c.gitOK(path, "diff", "--name-only", "--diff-filter=U")
			if err != nil {
				return "", err
			}
			if _, err := c.gitOK(path, "merge", "--abort"); err != nil {
				return "", err
			}
			if files == "" {
				return "", fail("bringing %s up to date with %s failed, and it was left as it was: %s", branch, onto, strings.TrimSpace(merged.Stdout+"\n"+merged.Stderr))
			}
			return "", fail("bringing %s up to date with %s conflicts in %s; it was left as it was, for someone to merge by hand", branch, onto, strings.Join(strings.Fields(files), ", "))
		}
		said = fmt.Sprintf("merged %s into %s", onto, branch)
	}
	if _, err := c.Ledger.AddEvent(top.ID, core.EventNote, said); err != nil {
		return "", err
	}
	return said, nil
}
