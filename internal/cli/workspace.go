package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"wd/internal/core"
	"wd/internal/project"
)

// workspace is one directory this director serves. A client names a workspace
// and never sends a path, which is what keeps a token from becoming filesystem
// access: the paths in this ledger were put there by whoever runs the director,
// not by whoever holds a token.
func (c *Cli) workspace(rest []string) error {
	sub := ""
	if len(rest) > 0 {
		sub = rest[0]
	}
	switch sub {
	case "add":
		return c.workspaceAdd(rest)
	case "create":
		return c.workspaceCreate(rest)
	case "list":
		return c.workspaceList()
	case "show":
		return c.workspaceShow(rest)
	case "":
		return c.workspaceList()
	default:
		return fail("usage: wd workspace (add <path> [--name label] [--creates] | create <parent> <name> | list | show <id-or-path>)")
	}
}

func (c *Cli) workspaceAdd(rest []string) error {
	if len(rest) < 2 {
		return fail("usage: wd workspace add <path> [--name <label>] [--creates]")
	}
	path, err := project.ExpandHome(rest[1])
	if err != nil {
		return err
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	if st, err := os.Stat(abs); err != nil {
		// A path that is not there yet is not refused, it is registered. A
		// workspace is somewhere work happens, and it can be created before
		// anything is in it. What is refused is a path that cannot be one.
		if !os.IsNotExist(err) {
			return fail("%s: %v", abs, err)
		}
	} else if !st.IsDir() {
		return fail("%s is not a directory", abs)
	}
	creates := flag(c.Args, "creates")
	name := strOr(c.Args, "name", "")
	if name == "" {
		name = filepath.Base(abs)
	}
	w, err := c.Ledger.AddWorkspace(name, abs, creates)
	if err != nil {
		return err
	}
	what := "workspace"
	if creates {
		what = "parent — new workspaces may be created under it"
	}
	return c.out(w, fmt.Sprintf("%s %s · %s", w.ID, what, w.Path))
}

func (c *Cli) workspaceCreate(rest []string) error {
	if len(rest) < 3 {
		return fail("usage: wd workspace create <parent> <name>")
	}
	parentID, name := rest[1], rest[2]
	// The containment check is the ledger's, not this command's: it is the same
	// check a remote client goes through, so a path that would be refused from a
	// phone is refused here too rather than being a hole reachable from the desk.
	path, err := c.Ledger.CreateWorkspacePath(parentID, name)
	if err != nil {
		return err
	}
	if _, err := os.Stat(path); err == nil {
		return fail("%s already exists: wd workspace add %s registers a directory that is already there", path, path)
	}
	if err := os.MkdirAll(path, 0o755); err != nil {
		return fail("%s: %v", path, err)
	}
	// A workspace is a git repository. Everything downstream — verify running in
	// a worktree, pr working out the pushed commit, delivery being derived from
	// git — assumes there is one, so a new workspace starts with one rather than
	// failing later at a gate that cannot explain itself.
	if err := gitInit(path); err != nil {
		return fail("%s: %v", path, err)
	}
	w, err := c.Ledger.AddWorkspace(name, path, false)
	if err != nil {
		return err
	}
	// Registering the project too is what makes "any project I start with wd" a
	// workspace without being entered twice: one directory, one row here and
	// one project file, both naming the same path.
	added := ""
	if _, err := c.project(name); err != nil {
		if p, perr := c.registerProject(w); perr == nil {
			added = fmt.Sprintf(" · registered project %s", p)
		}
	}
	return c.out(w, fmt.Sprintf("%s created · %s%s", w.ID, w.Path, added))
}

// gitInit makes path a repository. It says which git failed rather than
// swallowing it, because a workspace without one is not a workspace.
func gitInit(path string) error {
	cmd := exec.Command("git", "init", "--quiet")
	cmd.Dir = path
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git init: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (c *Cli) workspaceList() error {
	ws, err := c.Ledger.Workspaces()
	if err != nil {
		return err
	}
	out := make([]core.Workspace, 0, len(ws))
	table := make([]string, 0, len(ws))
	for _, w := range ws {
		out = append(out, w)
		kind := "workspace"
		if w.Creates {
			kind = "parent"
		}
		table = append(table, fmt.Sprintf("%s\t%s\t%s\t%s", w.ID, kind, w.Name, w.Path))
	}
	if len(table) == 0 {
		return c.out(out, "no workspaces: wd workspace add <path> --creates to say where new work may be created")
	}
	return c.out(out, strings.Join(table, "\n"))
}

func (c *Cli) workspaceShow(rest []string) error {
	if len(rest) < 2 {
		return fail("usage: wd workspace show <id-or-path>")
	}
	w, err := c.Ledger.Workspace(rest[1])
	if err != nil {
		return err
	}
	return c.out(w, fmt.Sprintf("%s\t%s\t%s\tcreates=%t", w.ID, w.Name, w.Path, w.Creates))
}

// registerProject writes the project file for a new workspace, so a project
// started here is a project the director already knows rather than one to
// register twice.
//
// Failure is not fatal to creating the workspace: the directory and its
// repository are the work, and the file can be written afterwards with
// `wd projects add`. Saying so is better than refusing, because the workspace
// is the thing that was asked for and it already exists on disk either way.
func (c *Cli) registerProject(w core.Workspace) (string, error) {
	if _, err := c.project(w.Name); err == nil {
		return w.Name, nil
	}
	if _, err := c.registerProjectFile(w.Name, w.Path, project.NewProjectOptions{}); err != nil {
		return "", err
	}
	return w.Name, nil
}
