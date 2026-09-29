package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// These cases pin failures the CLI once swallowed: each must fail loud, or
// record exactly what the user asked for.
func TestCommandsFailLoud(t *testing.T) {
	f := newCLIFixture(t)
	t1, t2, t3 := f.ids["t1"], f.ids["t2"], f.ids["t3"]

	t.Run("a project file that does not parse is never written", func(t *testing.T) {
		errStr := f.runFail(t, "projects", "add", "bogus-mode", f.sample, "--mode", "bogus", "--lazyspec", "n")
		if !strings.Contains(errStr, "bogus") {
			t.Fatalf("stderr = %q, want the bad mode named", errStr)
		}
		if _, err := os.Stat(filepath.Join(f.wdHome, "projects", "bogus-mode.md")); !os.IsNotExist(err) {
			t.Fatalf("project file left behind: %v", err)
		}
		f.runOK(t, "status")
	})

	t.Run("attach records the ref and cwd it is given", func(t *testing.T) {
		out := f.runOK(t, "attach", t3, "ses_ref", "--ref", "r1", "--cwd", f.sample, "--json")
		assertHasKey(t, out, `"ref": "r1"`)
		assertHasKey(t, out, `"cwd": "`+f.sample+`"`)
	})

	t.Run("set cannot skip the soft-done gate", func(t *testing.T) {
		f.runOK(t, "set", t2, "review")
		errStr := f.runFail(t, "set", t2, "soft-done")
		if !strings.Contains(errStr, "not ready for soft-done") {
			t.Fatalf("stderr = %q, want the soft-done gate", errStr)
		}
	})

	t.Run("a tilde path with no home directory fails", func(t *testing.T) {
		env := f.env(t, f.bin)
		for i, kv := range env {
			if strings.HasPrefix(kv, "HOME=") {
				env[i] = "HOME="
			}
		}
		for _, args := range [][]string{
			{"worktree", "attach", t3, "~/wt"},
			{"projects", "add", "homeless", "~/app", "--lazyspec", "n"},
		} {
			if code, out, errStr := f.runEnv(t, env, args...); code != 1 || !strings.Contains(errStr, "~/") {
				t.Errorf("wd %s: exit %d, stdout %q, stderr %q; want 1 naming the path", strings.Join(args, " "), code, out, errStr)
			}
		}
		if out := f.runOK(t, "worktree", "list", t3, "--json"); strings.Contains(out, "~") {
			t.Fatalf("a worktree was recorded at an unexpanded path: %s", out)
		}
	})

	t.Run("runner add refuses a name that is not a plain file name", func(t *testing.T) {
		file := filepath.Join(t.TempDir(), "spec.toml")
		if err := os.WriteFile(file, []byte(`spawn = "x run {brief}"`+"\n"+`session_id = 'session=(\w+)'`+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if errStr := f.runFail(t, "runner", "add", "../escape", file); !strings.Contains(errStr, "runner name") {
			t.Fatalf("stderr = %q, want the runner name refused", errStr)
		}
		if _, err := os.Stat(filepath.Join(f.wdHome, "escape.toml")); !os.IsNotExist(err) {
			t.Fatalf("a spec was written outside the runners dir: %v", err)
		}
	})

	t.Run("an unknown feedback subcommand fails", func(t *testing.T) {
		if errStr := f.runFail(t, "feedback", "ad", "x"); !strings.Contains(errStr, "usage: wd feedback") {
			t.Fatalf("stderr = %q, want feedback usage", errStr)
		}
	})

	t.Run("send refuses closed work before sending", func(t *testing.T) {
		if errStr := f.runFail(t, "send", t1, "hello"); !strings.Contains(errStr, "illegal transition done → running") {
			t.Fatalf("stderr = %q, want the illegal transition", errStr)
		}
		out := f.runOK(t, "events", t1, "--json")
		if strings.Contains(out, `"kind": "sent"`) {
			t.Fatalf("a sent event was recorded: %s", out)
		}
	})

	t.Run("open takes a path alone", func(t *testing.T) {
		code, out, errStr := f.runEnv(t, noEditorEnv(t, f), "open", "README.md")
		if code != 0 || !strings.Contains(out, "README.md") {
			t.Fatalf("exit %d, stdout %q, stderr %q; want the README link", code, out, errStr)
		}
	})

	t.Run("an editor that fails fails open", func(t *testing.T) {
		env := noEditorEnv(t, f)
		for i, kv := range env {
			if kv == "EDITOR=" {
				env[i] = "EDITOR=/usr/bin/false"
			}
		}
		code, _, errStr := f.runEnv(t, env, "open", "README.md")
		if code != 1 || !strings.Contains(errStr, "false") {
			t.Fatalf("exit %d, stderr %q; want 1 naming the editor", code, errStr)
		}
	})

	t.Run("verify with no commands fails without recording", func(t *testing.T) {
		f.runOK(t, "projects", "add", "unverified", f.sample, "--lazyspec", "n")
		out := f.runOK(t, "add", "unverified", "Unchecked", "--json")
		id := jsonString(t, out, "id")
		if errStr := f.runFail(t, "verify", id); !strings.Contains(errStr, "no verify commands") {
			t.Fatalf("stderr = %q, want no verify commands", errStr)
		}
		if out := f.runOK(t, "events", id, "--json"); strings.Contains(out, `"kind": "verify"`) {
			t.Fatalf("a verify event was recorded: %s", out)
		}
	})

	t.Run("a failing verify says so on stderr", func(t *testing.T) {
		f.runOK(t, "projects", "add", "failing", f.sample, "--verify", "false", "--lazyspec", "n")
		out := f.runOK(t, "add", "failing", "Broken", "--json")
		id := jsonString(t, out, "id")
		if errStr := f.runFail(t, "verify", id); !strings.Contains(errStr, "verify failed") {
			t.Fatalf("stderr = %q, want verify failed", errStr)
		}
	})

	t.Run("models with no list emit an empty array", func(t *testing.T) {
		out := f.runOK(t, "models", "claude", "--json")
		var rows map[string][]string
		if err := json.Unmarshal([]byte(out), &rows); err != nil {
			t.Fatalf("decode %s: %v", out, err)
		}
		if rows["claude"] == nil {
			t.Fatalf("models = %s, want claude: []", out)
		}
	})

	t.Run("without taste cards a brief fails naming WD_ROOT", func(t *testing.T) {
		var env []string
		for _, kv := range f.env(t, f.bin) {
			if !strings.HasPrefix(kv, "WD_ROOT=") {
				env = append(env, kv)
			}
		}
		code, _, errStr := f.runEnv(t, env, "brief", t3)
		if code != 1 || !strings.Contains(errStr, "WD_ROOT") {
			t.Fatalf("exit %d, stderr %q; want 1 naming WD_ROOT", code, errStr)
		}
	})
}

// noEditorEnv is the fixture environment with no editor on PATH, so open
// never launches the host's own.
func noEditorEnv(t *testing.T, f *cliFixture) []string {
	t.Helper()
	return f.envWithPath(t, f.pathWith(t, "claude", "opencode", "codex"))
}
