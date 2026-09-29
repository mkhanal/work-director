package context

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestWorkspaceContextTellsNotARepoFromAFailedProbe(t *testing.T) {
	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not on PATH")
	}

	outside := t.TempDir()
	ws, err := WorkspaceContext(outside)
	if err != nil || ws.Repo != nil {
		t.Fatalf("outside a repo: %+v, %v, want no repo and no error", ws, err)
	}

	repo := t.TempDir()
	if out, err := exec.Command(gitPath, "-C", repo, "init", "-q", "-b", "main").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	ws, err = WorkspaceContext(repo)
	if err != nil || ws.Repo == nil || ws.Branch == nil || *ws.Branch != "main" {
		t.Fatalf("inside a repo: %+v, %v, want repo on main", ws, err)
	}

	if _, err := WorkspaceContext(filepath.Join(outside, "deleted")); err == nil {
		t.Fatal("a missing directory reported as no repo")
	}

	t.Setenv("PATH", t.TempDir())
	if _, err := WorkspaceContext(repo); err == nil {
		t.Fatal("a missing git binary reported as no repo")
	}
}

func TestWorkspaceContextFailsWhenGitFailsForAnotherReason(t *testing.T) {
	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not on PATH")
	}
	repo := t.TempDir()
	if out, err := exec.Command(gitPath, "-C", repo, "init", "-q", "-b", "main").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	if err := os.WriteFile(filepath.Join(repo, ".git", "config"), []byte("[core\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if ws, err := WorkspaceContext(repo); err == nil {
		t.Fatalf("a repo git cannot read reported as %+v, want an error", ws)
	}
}
