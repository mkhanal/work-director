// Package context answers "where is the executor standing": the repo top,
// linked worktree path, current branch and dirty files, plus the one-line
// rendering the CLI prints.
package context

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
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

type gitResult struct {
	code   int
	stdout string
	stderr string
}

// git runs git in dir under the C locale, so its messages can be matched. A
// non-zero exit is a result; failing to run git at all is an error.
func git(dir string, args ...string) (gitResult, error) {
	c := exec.Command("git", args...)
	c.Dir = dir
	c.Env = append(os.Environ(), "LC_ALL=C")
	var stdout, stderr bytes.Buffer
	c.Stdout = &stdout
	c.Stderr = &stderr
	err := c.Run()
	r := gitResult{stdout: stdout.String(), stderr: strings.TrimSpace(stderr.String())}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		r.code = exitErr.ExitCode()
		return r, nil
	}
	if err != nil {
		return gitResult{}, fmt.Errorf("git in %s: %w", dir, err)
	}
	return r, nil
}

// gitOK runs a git probe that must succeed and returns its stdout.
func gitOK(dir string, args ...string) (string, error) {
	r, err := git(dir, args...)
	if err != nil {
		return "", err
	}
	if r.code != 0 {
		return "", fmt.Errorf("git %s in %s: exit %d: %s", strings.Join(args, " "), dir, r.code, r.stderr)
	}
	return r.stdout, nil
}

// WorkspaceContext probes dir with git: repo top, worktree list, branch and
// status. A directory outside any repo yields a Workspace with Repo nil; a
// directory or git binary that cannot be used is an error.
func WorkspaceContext(dir string) (Workspace, error) {
	r, err := git(dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return Workspace{}, err
	}
	if r.code != 0 {
		if strings.Contains(r.stderr, "not a git repository") {
			return Workspace{Dir: dir, Changed: []string{}}, nil
		}
		return Workspace{}, fmt.Errorf("git rev-parse --show-toplevel in %s: exit %d: %s", dir, r.code, r.stderr)
	}
	top := strings.TrimSpace(r.stdout)
	list, err := gitOK(dir, "worktree", "list", "--porcelain")
	if err != nil {
		return Workspace{}, err
	}
	mainRoot := firstWorktree(strings.Split(list, "\n\n")[0])
	branch, err := gitOK(dir, "branch", "--show-current")
	if err != nil {
		return Workspace{}, err
	}
	branch = strings.TrimSpace(branch)
	var actual *string
	if branch != "" {
		actual = &branch
	} else {
		// Detached HEAD, or no commits yet: --verify --quiet exits 1, silently,
		// when HEAD names no commit.
		head, err := git(dir, "rev-parse", "--short", "--verify", "--quiet", "HEAD")
		if err != nil {
			return Workspace{}, err
		}
		switch head.code {
		case 0:
			actual = ptr("detached " + strings.TrimSpace(head.stdout))
		case 1:
		default:
			return Workspace{}, fmt.Errorf("git rev-parse HEAD in %s: exit %d: %s", dir, head.code, head.stderr)
		}
	}
	status, err := gitOK(dir, "status", "--porcelain")
	if err != nil {
		return Workspace{}, err
	}
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
