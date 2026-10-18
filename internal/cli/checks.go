package cli

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"wd/internal/core"
	"wd/internal/project"
)

// gitIn runs git in dir with env added, returning what it printed and its
// exit code; err is only that git could not be run.
func gitIn(dir string, env []string, args ...string) (stdout, stderr string, code int, err error) {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), env...)
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	err = cmd.Run()
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return strings.TrimSpace(out.String()), strings.TrimSpace(errOut.String()), exit.ExitCode(), nil
	}
	return strings.TrimSpace(out.String()), strings.TrimSpace(errOut.String()), 0, err
}

// checkoutTree is the tree git would commit from dir's files as they are now,
// tracked and untracked alike and ignored ones left out. It is built in an
// index of its own, so the checkout's index is never touched. ok is false
// when dir is not in a git checkout.
func checkoutTree(dir string) (tree string, ok bool, err error) {
	if _, _, code, err := gitIn(dir, nil, "rev-parse", "--is-inside-work-tree"); err != nil || code != 0 {
		return "", false, err
	}
	index, stderr, code, err := gitIn(dir, nil, "rev-parse", "--path-format=absolute", "--git-path", "index")
	if err != nil {
		return "", false, err
	}
	if code != 0 {
		return "", false, fail("reading where %s keeps its index: %s", dir, stderr)
	}
	tmp, err := os.CreateTemp("", "wd-index-")
	if err != nil {
		return "", false, err
	}
	defer os.Remove(tmp.Name())
	// Starting from the checkout's own index keeps git from hashing every
	// file again; a checkout with no index yet starts from nothing.
	if src, err := os.Open(index); err == nil {
		_, err = io.Copy(tmp, src)
		src.Close()
		if err != nil {
			tmp.Close()
			return "", false, err
		}
	} else if !os.IsNotExist(err) {
		tmp.Close()
		return "", false, err
	}
	if err := tmp.Close(); err != nil {
		return "", false, err
	}
	env := []string{"GIT_INDEX_FILE=" + tmp.Name()}
	if _, stderr, code, err := gitIn(dir, env, "add", "-A"); err != nil || code != 0 {
		if err != nil {
			return "", false, err
		}
		return "", false, fail("reading the files of %s: %s", dir, stderr)
	}
	tree, stderr, code, err = gitIn(dir, env, "write-tree")
	if err != nil {
		return "", false, err
	}
	if code != 0 {
		return "", false, fail("reading the files of %s: %s", dir, stderr)
	}
	return tree, true, nil
}

// keepStart records the files dir holds as those work started from, when dir
// is a git checkout.
func (c *Cli) keepStart(work, dir string) error {
	tree, ok, err := checkoutTree(dir)
	if err != nil || !ok {
		return err
	}
	return c.Ledger.StartedAt(work, tree)
}

// changedSinceStart is whether t's checkout holds other files than when t
// started. Work whose start was not kept counts as changed: only the
// checkout can say it was not.
func (c *Cli) changedSinceStart(t core.Work, dir string) (bool, error) {
	start, ok, err := c.Ledger.Started(t.ID)
	if err != nil || !ok {
		return true, err
	}
	now, ok, err := checkoutTree(dir)
	if err != nil || !ok {
		return true, err
	}
	return now != start, nil
}

// noChangesNote is the note a task that changed nothing closes with.
const noChangesNote = "closed: no changes to check"

// checkName is how a person calls a check: the script a package manager runs,
// else the command.
func checkName(cmd string) string {
	f := strings.Fields(cmd)
	if len(f) >= 3 && f[1] == "run" && (f[0] == "npm" || f[0] == "pnpm" || f[0] == "yarn" || f[0] == "bun") {
		return strings.Join(f[2:], " ")
	}
	return cmd
}

// checkToClose runs the project's checks on t before it closes, and returns
// why it may not close, or "". A check that fails on t's work fails it only
// when it does not fail the same way on the commit t's checkout branched from
// main at: a project whose own checks fail on main cannot hold every task to
// them, so the goal holds that as one concern instead.
func (c *Cli) checkToClose(t core.Work, p *project.Project, dir string) (string, error) {
	pass, body, runs, err := c.runVerify(t.ID)
	if err != nil {
		return "", err
	}
	top, err := c.topGoal(t)
	if err != nil {
		return "", err
	}
	if pass {
		return "", c.checksPassAgain(top)
	}
	var failing []string
	for _, r := range runs {
		if r.Code != 0 {
			failing = append(failing, r.Cmd)
		}
	}
	onMain, why, err := c.onMain(p, dir, failing)
	if err != nil {
		return "", err
	}
	var mains, own []string
	for _, r := range runs {
		if r.Code == 0 {
			continue
		}
		m, ran := onMain[r.Cmd]
		switch {
		case !ran:
		case m.Code == r.Code:
			mains = append(mains, r.Cmd)
		case m.Code == 0:
			own = append(own, fmt.Sprintf("`%s` passes on main", r.Cmd))
		default:
			own = append(own, fmt.Sprintf("`%s` fails differently on main (exit %d there, %d here)", r.Cmd, m.Code, r.Code))
		}
	}
	if why == "" && len(own) == 0 {
		if _, err := c.Ledger.AddEvent(t.ID, core.EventNote, "closed with checks that fail on main too: "+strings.Join(mains, ", ")); err != nil {
			return "", err
		}
		return "", c.raiseChecksConcern(top, mains)
	}
	held := body
	if why != "" {
		held += "\ncould not run them on main to compare: " + why
	} else {
		held += "\n" + strings.Join(own, "\n")
	}
	if _, err := c.Ledger.AddEvent(t.ID, core.EventNote, stoppedAtNote+"verify\n"+held); err != nil {
		return "", err
	}
	if why != "" {
		return "its checks fail, and running them on main to compare could not be done (" + clipLine(why, 160) + ")", nil
	}
	return "its checks fail on its work: " + strings.Join(own, "; "), nil
}

// raiseChecksConcern has goal hold the concern that the checks cmds fail on
// main, unless it already does.
func (c *Cli) raiseChecksConcern(goal core.Work, cmds []string) error {
	open, err := c.openChecksConcerns(goal)
	if err != nil || len(open) > 0 {
		return err
	}
	names := make([]string, len(cmds))
	for i, cmd := range cmds {
		names[i] = checkName(cmd)
	}
	_, err = c.Ledger.AddConcern(goal.ID, core.ChecksConcern(names))
	return err
}

// checksPassAgain resolves goal's concern that the checks fail on main, now
// that a task's checks passed in full.
func (c *Cli) checksPassAgain(goal core.Work) error {
	open, err := c.openChecksConcerns(goal)
	if err != nil {
		return err
	}
	for _, cx := range open {
		if _, err := c.Ledger.ResolveConcern(cx.ID, "the checks pass again"); err != nil {
			return err
		}
	}
	return nil
}

func (c *Cli) openChecksConcerns(goal core.Work) ([]core.Concern, error) {
	all, err := c.Ledger.Concerns(&goal.ID)
	if err != nil {
		return nil, err
	}
	var out []core.Concern
	for _, cx := range all {
		if cx.Resolved == 0 && strings.HasPrefix(cx.Text, core.ChecksFailOnMain) {
			out = append(out, cx)
		}
	}
	return out, nil
}

// onMain runs checks on the commit dir's checkout branched from the project's
// default branch at, in a checkout of its own with its own dependencies,
// removed afterwards. It returns each run by its command, or why they could
// not be run there.
func (c *Cli) onMain(p *project.Project, dir string, checks []string) (map[string]checkRun, string, error) {
	base, stderr, code, err := gitIn(dir, nil, "merge-base", "HEAD", p.DefaultBranch)
	if err != nil {
		return nil, "", err
	}
	if code != 0 {
		return nil, fmt.Sprintf("no commit in common with %s: %s", p.DefaultBranch, stderr), nil
	}
	tmp, err := os.MkdirTemp("", "wd-main-checks-")
	if err != nil {
		return nil, "", err
	}
	defer os.RemoveAll(tmp)
	path := filepath.Join(tmp, "main")
	if _, stderr, code, err := gitIn(dir, nil, "worktree", "add", "--detach", path, base); err != nil || code != 0 {
		if err != nil {
			return nil, "", err
		}
		return nil, "a checkout of main could not be made: " + stderr, nil
	}
	// The checkout is scratch under tmp, removed with it; prune drops what
	// git still records of it whether or not remove managed to.
	defer func() {
		gitIn(dir, nil, "worktree", "remove", "--force", path)
		gitIn(dir, nil, "worktree", "prune")
	}()
	if err := c.installIn(path, p); err != nil {
		return nil, err.Error(), nil
	}
	runs, err := runChecks(checks, path)
	if err != nil {
		return nil, "", err
	}
	out := map[string]checkRun{}
	for _, r := range runs {
		out[r.Cmd] = r
	}
	return out, "", nil
}
