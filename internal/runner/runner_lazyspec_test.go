package runner

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

const testSID = "b765c0a2-616a-4662-b7f3-c566a03f0db5"

func strPtr(s string) *string { return &s }

// writeFake installs a fake CLI: it records its argv to <bin>/calls.log and
// answers like the real one did on 2026-09-04.
func writeFake(t *testing.T, bin, name, body string) {
	t.Helper()
	script := fmt.Sprintf("#!/usr/bin/env bash\necho \"%s $*\" >> %q\n%s\n", name, filepath.Join(bin, "calls.log"), body)
	if err := os.WriteFile(filepath.Join(bin, name), []byte(script), 0o755); err != nil {
		t.Fatalf("write fake %s: %v", name, err)
	}
}

func calls(t *testing.T, bin string) string {
	t.Helper()
	text, err := os.ReadFile(filepath.Join(bin, "calls.log"))
	if err != nil {
		return ""
	}
	return string(text)
}

// waitForCall waits for a detached process to record its argv.
func waitForCall(t *testing.T, bin, substr string) {
	t.Helper()
	_, ok := waitFor(func() (string, bool) {
		if strings.Contains(calls(t, bin), substr) {
			return substr, true
		}
		return "", false
	}, 5*time.Second, 20*time.Millisecond)
	if !ok {
		t.Fatalf("call %q never reached calls.log", substr)
	}
}

func TestRunner(t *testing.T) {
	bin := t.TempDir()
	home := t.TempDir()
	cwdResolved, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("resolve cwd: %v", err)
	}
	cwd := cwdResolved
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("HOME", home)
	t.Setenv("WD_HOME", filepath.Join(home, ".work-director"))

	writeFake(t, bin, "claude", `case "$1 $2" in
  "--bg -n") echo "backgrounded · b765c0a2 · $3";;
  "--bg --resume") echo "backgrounded · b765c0a2";;
  "agents --json") echo '[{"id":"b765c0a2","sessionId":"`+testSID+`","cwd":"`+cwd+`","kind":"background","status":"idle","state":"done"}]';;
esac`)
	writeFake(t, bin, "opencode", `case "$1" in
  run) echo '{"type":"step_start","sessionID":"ses_abc123"}'; echo '{"type":"text","sessionID":"ses_abc123","part":{"type":"text","text":"READY"}}';;
  session) echo '{"info":{"id":"ses_abc123"},"messages":[{"type":"user"},{"type":"assistant","content":[{"type":"text","text":"READY"}]},{"type":"assistant","content":[{"type":"text","text":"PONG"}]}]}';;
  models) echo 'anthropic/claude-opus-5'; echo 'openai/gpt-5.2';;
esac`)
	writeFake(t, bin, "codex", `case "$1" in
  exec) echo '{"type":"thread","session_id":"codex-abc123","thread":{"messages":[]}}';;
  debug) echo 'codex/opus-5'; echo 'codex/sonnet-4-5';;
esac`)
	writeFake(t, bin, "myagent", `case "$1" in
  run) echo "session=victory-001";;
  send) echo ok;;
  status) echo 'state: waiting';;
  export) echo 'row one'; echo 'row two';;
  models) echo 'victory/1';;
esac`)

	claudeDir := filepath.Join(home, ".claude", "projects", projectSlug(cwd))
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		t.Fatalf("mkdir claude projects: %v", err)
	}
	transcript := strings.Join([]string{
		`{"type":"user","message":{"content":"go"}}`,
		`{"type":"assistant","message":{"content":[{"type":"text","text":"READY"}]}}`,
		`{"type":"assistant","message":{"content":[{"type":"text","text":"STATUS: DONE"}]}}`,
	}, "\n")
	if err := os.WriteFile(filepath.Join(claudeDir, testSID+".jsonl"), []byte(transcript), 0o644); err != nil {
		t.Fatalf("write transcript: %v", err)
	}

	specDir := filepath.Join(home, ".work-director", "runners")
	if err := os.MkdirAll(specDir, 0o755); err != nil {
		t.Fatalf("mkdir runners: %v", err)
	}
	myagent := `name = "myagent"
spawn = "myagent run --name {name} {brief}"
session_id = 'session=([A-Za-z0-9-]+)'
send = "myagent send {session} {text}"
status = "myagent status {session}"
running = 'state: *running'
waiting = 'state: *waiting'
exited = 'state: *exited'
transcript = "myagent export {session}"
models = "myagent models"
attach = "myagent attach {session}"
`
	if err := os.WriteFile(filepath.Join(specDir, "myagent.toml"), []byte(myagent), 0o644); err != nil {
		t.Fatalf("write myagent.toml: %v", err)
	}

	t.Run("Spawning Records Runner Session And Attach Hint", func(t *testing.T) {
		t.Run("claude: session id from claude agents, hint uses bg id", func(t *testing.T) {
			h, err := claude.Spawn(SpawnOptions{Cwd: cwd, Name: "wd-1 t", Brief: "B"})
			if err != nil {
				t.Fatalf("claude spawn: %v", err)
			}
			if h.Runner != "claude" || h.Session != testSID || h.Cwd != cwd {
				t.Fatalf("handle = %+v, want claude/%s in %s", h, testSID, cwd)
			}
			if h.Ref == nil || *h.Ref != "b765c0a2" {
				t.Fatalf("ref = %v, want b765c0a2", h.Ref)
			}
			if got := claude.AttachHint(h); got != "claude attach b765c0a2" {
				t.Fatalf("attachHint = %q, want %q", got, "claude attach b765c0a2")
			}
			if !strings.Contains(calls(t, bin), "claude --bg -n wd-1 t --permission-mode bypassPermissions B") {
				t.Fatalf("calls.log missing claude spawn:\n%s", calls(t, bin))
			}
		})
		t.Run("opencode: session id from json stream, hint resumes it", func(t *testing.T) {
			h, err := opencode.Spawn(SpawnOptions{Cwd: cwd, Name: "wd-2 t", Brief: "B"})
			if err != nil {
				t.Fatalf("opencode spawn: %v", err)
			}
			if h.Session != "ses_abc123" {
				t.Fatalf("session = %q, want ses_abc123", h.Session)
			}
			want := "cd " + cwd + " && opencode -s ses_abc123"
			if got := opencode.AttachHint(h); got != want {
				t.Fatalf("attachHint = %q, want %q", got, want)
			}
		})
	})

	t.Run("Sending Continues The Same Session", func(t *testing.T) {
		t.Run("claude resumes by session id without other flags", func(t *testing.T) {
			h := Handle{Runner: "claude", Session: testSID, Ref: strPtr("b765c0a2"), Cwd: cwd}
			if err := claude.Send(&h, "more"); err != nil {
				t.Fatalf("claude send: %v", err)
			}
			if !strings.Contains(calls(t, bin), "claude --bg --resume "+testSID+" more") {
				t.Fatalf("calls.log missing claude send:\n%s", calls(t, bin))
			}
		})
		t.Run("opencode resumes with -s", func(t *testing.T) {
			h := Handle{Runner: "opencode", Session: "ses_abc123", Cwd: cwd}
			if err := opencode.Send(&h, "more"); err != nil {
				t.Fatalf("opencode send: %v", err)
			}
			waitForCall(t, bin, "opencode run --format json --auto -s ses_abc123 more")
		})
	})

	t.Run("A Transcript Yields The Executor Messages", func(t *testing.T) {
		t.Run("claude reads the jsonl under ~/.claude/projects/<slug>", func(t *testing.T) {
			h := Handle{Runner: "claude", Session: testSID, Ref: strPtr("b765c0a2"), Cwd: cwd}
			got, err := claude.Transcript(h)
			if err != nil {
				t.Fatalf("claude transcript: %v", err)
			}
			if !slices.Equal(got, []string{"READY", "STATUS: DONE"}) {
				t.Fatalf("transcript = %v, want [READY STATUS: DONE]", got)
			}
		})
		t.Run("opencode reads export", func(t *testing.T) {
			h := Handle{Runner: "opencode", Session: "ses_abc123", Cwd: cwd}
			got, err := opencode.Transcript(h)
			if err != nil {
				t.Fatalf("opencode transcript: %v", err)
			}
			if !slices.Equal(got, []string{"READY", "PONG"}) {
				t.Fatalf("transcript = %v, want [READY PONG]", got)
			}
		})
	})

	t.Run("A Runner Forwards The Chosen Model", func(t *testing.T) {
		t.Run("claude passes --model on spawn", func(t *testing.T) {
			if _, err := claude.Spawn(SpawnOptions{Cwd: cwd, Name: "wd-1 t", Brief: "B", Model: strPtr("fable")}); err != nil {
				t.Fatalf("claude spawn: %v", err)
			}
			if !strings.Contains(calls(t, bin), "claude --bg -n wd-1 t --permission-mode bypassPermissions --model fable B") {
				t.Fatalf("calls.log missing claude model spawn:\n%s", calls(t, bin))
			}
		})
		t.Run("codex passes --model and records the session from its jsonl", func(t *testing.T) {
			h, err := codex.Spawn(SpawnOptions{Cwd: cwd, Name: "wd-4 t", Brief: "B", Model: strPtr("opus")})
			if err != nil {
				t.Fatalf("codex spawn: %v", err)
			}
			if h.Session != "codex-abc123" {
				t.Fatalf("session = %q, want codex-abc123", h.Session)
			}
			if !strings.Contains(calls(t, bin), "codex exec --cd "+cwd+" --json --full-auto --model opus B") {
				t.Fatalf("calls.log missing codex model spawn:\n%s", calls(t, bin))
			}
		})
		t.Run("a spec runner whose spawn line has no {model} fails loud", func(t *testing.T) {
			r, err := RunnerNamed("myagent")
			if err != nil {
				t.Fatalf("RunnerNamed: %v", err)
			}
			_, err = r.Spawn(SpawnOptions{Cwd: cwd, Name: "wd-3 t", Brief: "B", Model: strPtr("fable")})
			if err == nil || !strings.Contains(err.Error(), "model") {
				t.Fatalf("err = %v, want a loud model refusal", err)
			}
		})
	})

	t.Run("Model Lists Come From The Provider CLI, Not A Registry", func(t *testing.T) {
		t.Run("opencode and codex return whatever their own CLI prints", func(t *testing.T) {
			got, err := opencode.Models()
			if err != nil {
				t.Fatalf("opencode models: %v", err)
			}
			if !slices.Equal(got, []string{"anthropic/claude-opus-5", "openai/gpt-5.2"}) {
				t.Fatalf("opencode models = %v", got)
			}
			got, err = codex.Models()
			if err != nil {
				t.Fatalf("codex models: %v", err)
			}
			if !slices.Equal(got, []string{"codex/opus-5", "codex/sonnet-4-5"}) {
				t.Fatalf("codex models = %v", got)
			}
		})
		t.Run("claude has no CLI list and returns none, never a guess", func(t *testing.T) {
			got, err := claude.Models()
			if err != nil {
				t.Fatalf("claude models: %v", err)
			}
			if !slices.Equal(got, []string{}) {
				t.Fatalf("claude models = %v, want none", got)
			}
		})
	})

	t.Run("Every Registered Runner Reports Whether It Is Detected", func(t *testing.T) {
		only := t.TempDir()
		for _, name := range []string{"claude", "myagent"} {
			if err := os.Symlink(filepath.Join(bin, name), filepath.Join(only, name)); err != nil {
				t.Fatalf("link %s: %v", name, err)
			}
		}
		t.Setenv("PATH", only)
		list, err := Availabilities()
		if err != nil {
			t.Fatalf("availabilities: %v", err)
		}
		got := map[string]Availability{}
		for _, a := range list {
			got[a.Runner] = a
		}
		for _, name := range []string{"claude", "myagent"} {
			if a := got[name]; !a.Detected || a.Path == nil || *a.Path != filepath.Join(only, name) {
				t.Errorf("%s = %+v, want detected in %s", name, a, only)
			}
		}
		for _, name := range []string{"opencode", "codex"} {
			if a, ok := got[name]; !ok || a.Detected || a.Path != nil || a.Command != name {
				t.Errorf("%s = %+v (listed %v), want not detected", name, a, ok)
			}
		}
	})

	t.Run("Only A Detected Runner Resolves For Spawning", func(t *testing.T) {
		only := t.TempDir()
		if err := os.Symlink(filepath.Join(bin, "claude"), filepath.Join(only, "claude")); err != nil {
			t.Fatalf("link claude: %v", err)
		}
		t.Setenv("PATH", only)
		if _, err := DetectedRunner("claude"); err != nil {
			t.Fatalf("claude on PATH: %v", err)
		}
		for _, name := range []string{"codex", "myagent"} {
			_, err := DetectedRunner(name)
			if err == nil {
				t.Fatalf("%s resolved with its command off PATH", name)
			}
			for _, want := range []string{name, "not found on PATH", "wd runner init"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("%s error %q does not contain %q", name, err, want)
				}
			}
		}
	})

	t.Run("A Runner Can Be Defined By A File Of Commands", func(t *testing.T) {
		t.Run("a ~/.work-director/runners/*.toml drives spawn, send, status, transcript, models", func(t *testing.T) {
			known, err := AllRunnerNames()
			if err != nil {
				t.Fatalf("allRunnerNames: %v", err)
			}
			slices.Sort(known)
			if !slices.Equal(known, []string{"claude", "codex", "myagent", "opencode"}) {
				t.Fatalf("runners = %v, want [claude codex myagent opencode]", known)
			}
			r, err := RunnerNamed("myagent")
			if err != nil {
				t.Fatalf("RunnerNamed: %v", err)
			}
			h, err := r.Spawn(SpawnOptions{Cwd: cwd, Name: "wd-8 t", Brief: "B"})
			if err != nil {
				t.Fatalf("myagent spawn: %v", err)
			}
			if h.Session != "victory-001" {
				t.Fatalf("session = %q, want victory-001", h.Session)
			}
			if got := r.AttachHint(h); got != "myagent attach 'victory-001'" {
				t.Fatalf("attachHint = %q, want %q", got, "myagent attach 'victory-001'")
			}
			if err := r.Send(&h, "more"); err != nil {
				t.Fatalf("myagent send: %v", err)
			}
			if !strings.Contains(calls(t, bin), "myagent run --name wd-8 t B") {
				t.Fatalf("calls.log missing myagent spawn:\n%s", calls(t, bin))
			}
			if !strings.Contains(calls(t, bin), "myagent send victory-001 more") {
				t.Fatalf("calls.log missing myagent send:\n%s", calls(t, bin))
			}
			status, err := r.Status(h)
			if err != nil {
				t.Fatalf("myagent status: %v", err)
			}
			if status != StatusWaiting {
				t.Fatalf("status = %q, want waiting", status)
			}
			texts, err := r.Transcript(h)
			if err != nil {
				t.Fatalf("myagent transcript: %v", err)
			}
			if !slices.Equal(texts, []string{"row one", "row two"}) {
				t.Fatalf("transcript = %v, want [row one row two]", texts)
			}
			models, err := r.Models()
			if err != nil {
				t.Fatalf("myagent models: %v", err)
			}
			if !slices.Equal(models, []string{"victory/1"}) {
				t.Fatalf("models = %v, want [victory/1]", models)
			}
		})
		t.Run("an unknown runner name fails loudly with guidance", func(t *testing.T) {
			_, err := RunnerNamed("cursor")
			if err == nil || !strings.Contains(err.Error(), "cursor") {
				t.Fatalf("err = %v, want a loud unknown-runner failure", err)
			}
		})
	})
}
