package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"wd/internal/runner"
)

// Every requirement in doctor.lazyspec.md is married to a test here. Each
// runs the built CLI with a controlled PATH and working directory.

// doctorReport runs `wd doctor --json` in dir with PATH set to path.
func doctorReport(t *testing.T, f *cliFixture, dir, path string) Doctor {
	t.Helper()
	code, out, errStr := f.runAt(t, dir, f.envWithPath(t, path), "doctor", "--json")
	if code != 0 {
		t.Fatalf("doctor exited %d\n%s\n%s", code, out, errStr)
	}
	var d Doctor
	if err := json.Unmarshal([]byte(out), &d); err != nil {
		t.Fatalf("doctor --json: %v\n%s", err, out)
	}
	return d
}

func TestDoctor(t *testing.T) {
	f := newCLIFixture(t)
	t.Run("Doctor Lists Every Registered Runner As Detected Or Not", func(t *testing.T) {
		// PATH holds git plus the claude and myagent fakes only.
		path := gitOnlyPath(t)
		for _, name := range []string{"claude", "myagent"} {
			if err := os.Symlink(filepath.Join(f.bin, name), filepath.Join(path, name)); err != nil {
				t.Fatalf("link %s: %v", name, err)
			}
		}
		got := map[string]runner.Availability{}
		for _, a := range doctorReport(t, f, f.sample, path).Runners {
			got[a.Runner] = a
		}
		for _, name := range []string{"claude", "myagent"} {
			a := got[name]
			if !a.Detected || a.Path == nil || *a.Path != filepath.Join(path, name) {
				t.Errorf("%s = %+v, want detected at %s", name, a, filepath.Join(path, name))
			}
		}
		for _, name := range []string{"opencode", "codex", "ao", "planner", "advisor"} {
			a, ok := got[name]
			if !ok {
				t.Errorf("%s not listed", name)
				continue
			}
			if a.Detected || a.Path != nil || a.Command != name {
				t.Errorf("%s = %+v, want not detected, command %s", name, a, name)
			}
		}
		code, out, _ := f.runAt(t, f.sample, f.envWithPath(t, path), "doctor")
		if code != 0 || !strings.Contains(out, "opencode   not detected") {
			t.Fatalf("doctor text = %q", out)
		}
	})

	t.Run("Doctor Reports Whether The Directory Is A Repo", func(t *testing.T) {
		inside := doctorReport(t, f, f.sample, gitOnlyPath(t)).Workspace
		if inside.Repo == nil || inside.Branch == nil || *inside.Branch != "main" {
			t.Fatalf("inside the sample repo: %+v", inside)
		}
		plain := t.TempDir()
		outside := doctorReport(t, f, plain, gitOnlyPath(t)).Workspace
		if outside.Repo != nil {
			t.Fatalf("outside any repo: repo = %s", *outside.Repo)
		}
		_, out, _ := f.runAt(t, plain, f.envWithPath(t, gitOnlyPath(t)), "doctor")
		if !strings.Contains(out, "(no git repo)") {
			t.Fatalf("doctor text = %q", out)
		}
	})

	t.Run("Outside A Repo Doctor Offers Git Init And Never Runs It", func(t *testing.T) {
		plain := t.TempDir()
		d := doctorReport(t, f, plain, gitOnlyPath(t))
		if d.Init == nil || *d.Init != "git init" {
			t.Fatalf("init = %v, want git init offered", d.Init)
		}
		if _, err := os.Stat(filepath.Join(plain, ".git")); !os.IsNotExist(err) {
			t.Fatalf("doctor created %s/.git", plain)
		}
		_, out, _ := f.runAt(t, plain, f.envWithPath(t, gitOnlyPath(t)), "doctor")
		if !strings.Contains(out, "git init") {
			t.Fatalf("doctor text does not offer git init: %q", out)
		}
		if in := doctorReport(t, f, f.sample, gitOnlyPath(t)); in.Init != nil {
			t.Fatalf("inside a repo init = %s, want none", *in.Init)
		}
	})
}
