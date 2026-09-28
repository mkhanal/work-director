// Package context answers "where is the executor standing": the repo top,
// linked worktree path, current branch and dirty files, plus the one-line
// rendering the CLI prints. It is a port of packages/wd/src/context.ts with
// the same git commands and the same output.
package context

import (
	"strconv"
	"strings"

	"wd/internal/core"
	"wd/internal/ledger"
	"wd/internal/runner"
)

// Workspace is the director's view of a directory: which repo it is in,
// whether it is a linked worktree, the branch, and up to 12 dirty files.
type Workspace struct {
	Dir     string   `json:"dir"`
	Repo    *string  `json:"repo"`
	Linked  bool     `json:"linked"`
	Branch  *string  `json:"branch"`
	Changed []string `json:"changed"`
}

// WorkspaceContext probes dir with git: repo top, worktree list, branch and
// status. A directory outside any repo yields a Workspace with Repo nil.
func WorkspaceContext(dir string) (Workspace, error) {
	git := func(args ...string) (int, string) {
		r, err := runner.Run(append([]string{"git"}, args...), dir)
		if err != nil {
			return 1, ""
		}
		return r.Code, strings.TrimSpace(r.Stdout)
	}
	topCode, top := git("rev-parse", "--show-toplevel", "--quiet")
	if topCode != 0 || top == "" {
		return Workspace{Dir: dir, Changed: []string{}}, nil
	}
	_, list := git("worktree", "list", "--porcelain")
	mainRoot := ""
	if blocks := strings.Split(list, "\n\n"); len(blocks) > 0 {
		mainRoot = firstWorktree(blocks[0])
	}
	branchCode, branch := git("branch", "--show-current")
	_, short := git("rev-parse", "--short", "HEAD")
	var actual *string
	if branchCode == 0 && branch != "" {
		actual = &branch
	} else if short == "" {
		actual = nil
	} else {
		actual = ptr("detached " + short)
	}
	_, status := git("status", "--porcelain")
	changed := []string{}
	for _, s := range strings.Split(status, "\n") {
		if s != "" {
			changed = append(changed, s)
		}
	}
	if len(changed) > 12 {
		changed = changed[:12]
	}
	linked := mainRoot != "" && top != mainRoot
	return Workspace{Dir: dir, Repo: &top, Linked: linked, Branch: actual, Changed: changed}, nil
}

func firstWorktree(block string) string {
	for _, line := range strings.Split(block, "\n") {
		if strings.HasPrefix(line, "worktree ") {
			return strings.TrimPrefix(line, "worktree ")
		}
	}
	return ""
}

func ptr(s string) *string { return &s }

// ContextLine renders the one-line workspace summary.
func ContextLine(w Workspace) string {
	if w.Repo == nil {
		return "dir: " + w.Dir + " (no git repo)"
	}
	where := ""
	if w.Linked {
		where = " · worktree: " + *w.Repo
	}
	branch := "no commits"
	if w.Branch != nil {
		branch = *w.Branch
	}
	return "dir: " + w.Dir + " · branch: " + branch + where + " · " + strconv.Itoa(len(w.Changed)) + " changed"
}

// EffectiveCwd is the directory an executor works in: its registered cwd, else
// the epic's shared worktree, else ".".
func EffectiveCwd(w core.Work, l *ledger.Ledger) string {
	if w.Cwd != nil {
		return *w.Cwd
	}
	shared := func(workID string) *string {
		wts, err := l.Worktrees(workID)
		if err != nil {
			return nil
		}
		for _, wt := range wts {
			if wt.Kind == core.WorktreeShared && wt.State == core.WorktreeActive {
				return &wt.Path
			}
		}
		return nil
	}
	if core.IsEpic(w.Kind) {
		if p := shared(w.ID); p != nil {
			return *p
		}
	} else if w.Parent != nil {
		if p := shared(*w.Parent); p != nil {
			return *p
		}
	}
	return "."
}
