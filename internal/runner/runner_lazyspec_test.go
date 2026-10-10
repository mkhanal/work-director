package runner

import (
	"fmt"
	"os"
	"os/exec"
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
	_, ok, err := waitFor(func() (string, bool, error) {
		return substr, strings.Contains(calls(t, bin), substr), nil
	}, 5*time.Second, 20*time.Millisecond)
	if err != nil || !ok {
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
  "stop deadbeef") echo 'no background session deadbeef' >&2; exit 1;;
  "stop "*) echo "stopped $2";;
esac`)
	writeFake(t, bin, "opencode", `case "$1" in
  run) echo '{"type":"step_start","sessionID":"ses_abc123"}'; echo '{"type":"text","sessionID":"ses_abc123","part":{"type":"text","text":"READY"}}';;
  session) echo '{"info":{"id":"ses_abc123"},"messages":[{"type":"user"},{"type":"assistant","content":[{"type":"text","text":"READY"}]},{"type":"assistant","content":[{"type":"text","text":"PONG"}]}]}';;
  models) echo 'anthropic/claude-opus-5'; echo 'openai/gpt-5.2';;
esac`)
	writeFake(t, bin, "codex", `case "$1" in
  exec) case "$*" in
      *resume*) echo '{"type":"rollup","session_id":"codex-abc123","rollup":{"summary":"PONG"}}';;
      *) echo '{"type":"thread","session_id":"codex-abc123","thread":{"messages":[{"role":"assistant","content":[{"type":"text","text":"READY"}]}]}}';;
    esac;;
  debug) echo 'codex/opus-5'; echo 'codex/sonnet-4-5';;
esac`)
	writeFake(t, bin, "myagent", `case "$1" in
  run) echo "session=victory-001";;
  send) echo ok;;
  status) [ "$2" = dead ] && { echo 'no such session' >&2; exit 3; }; echo 'state: waiting';;
  export) echo 'row one'; echo 'row two';;
  models) echo 'victory/1';;
  stop) echo stopped;;
esac`)

	claudeDir := filepath.Join(home, ".claude", "projects", projectSlug(cwd))
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		t.Fatalf("mkdir claude projects: %v", err)
	}
	transcript := strings.Join([]string{
		`{"type":"user","message":{"content":"go"}}`,
		`{"type":"assistant","message":{"content":[{"type":"text","text":"READY"}]}}`,
		`{"type":"assistant","message":{"content":[{"type":"text","text":"STATUS: DONE"}]}}`,
		``,
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
transcript = "myagent export {session} {slug_cwd}"
models = "myagent models"
attach = "myagent attach {session}"
stop = "myagent stop {session}"
`
	if err := os.WriteFile(filepath.Join(specDir, "myagent.toml"), []byte(myagent), 0o644); err != nil {
		t.Fatalf("write myagent.toml: %v", err)
	}
	detached := `spawn = "myagent run --name {name} {brief}"
detach = true
session_id = 'session=([A-Za-z0-9-]+)'
transcript = "cat {log}"
`
	if err := os.WriteFile(filepath.Join(specDir, "detached.toml"), []byte(detached), 0o644); err != nil {
		t.Fatalf("write detached.toml: %v", err)
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
		t.Run("codex reads every turn's recorded stream, from any process", func(t *testing.T) {
			h, err := codex.Spawn(SpawnOptions{Cwd: cwd, Name: "wd-5 t", Brief: "B"})
			if err != nil {
				t.Fatalf("codex spawn: %v", err)
			}
			if err := codex.Send(&h, "more"); err != nil {
				t.Fatalf("codex send: %v", err)
			}
			fresh := codexRunner{}
			got, ok, err := waitFor(func() ([]string, bool, error) {
				got, err := fresh.Transcript(h)
				return got, len(got) == 2, err
			}, 5*time.Second, 20*time.Millisecond)
			if err != nil {
				t.Fatalf("codex transcript: %v", err)
			}
			if !ok || !slices.Equal(got, []string{"READY", "PONG"}) {
				t.Fatalf("transcript = %v, want [READY PONG]", got)
			}
		})
	})

	t.Run("A Transcript Follows Its Session Into A Worktree", func(t *testing.T) {
		moved := "0d7a1c52-5b8e-4f0e-9c1a-2f3b4c5d6e7f"
		dir := filepath.Join(home, ".claude", "projects", projectSlug(filepath.Join(cwd, ".claude", "worktrees", "fix")))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir worktree project: %v", err)
		}
		if err := os.WriteFile(filepath.Join(dir, moved+".jsonl"), []byte(transcript), 0o644); err != nil {
			t.Fatalf("write moved transcript: %v", err)
		}
		got, err := claude.Transcript(Handle{Runner: "claude", Session: moved, Cwd: cwd})
		if err != nil {
			t.Fatalf("claude transcript: %v", err)
		}
		if !slices.Equal(got, []string{"READY", "STATUS: DONE"}) {
			t.Fatalf("transcript = %v, want [READY STATUS: DONE]", got)
		}
	})

	t.Run("A Transcript Skips A Line Still Being Written", func(t *testing.T) {
		codexDir := filepath.Join(home, ".work-director", "codex")
		if err := os.MkdirAll(codexDir, 0o755); err != nil {
			t.Fatalf("mkdir codex: %v", err)
		}
		cases := []struct {
			runner   Runner
			handle   Handle
			log      string
			complete string
			torn     string
		}{
			{
				runner:   claude,
				handle:   Handle{Runner: "claude", Session: "5e1f2a3b-0c4d-4e5f-8a6b-7c8d9e0f1a2b", Cwd: cwd},
				log:      filepath.Join(claudeDir, "5e1f2a3b-0c4d-4e5f-8a6b-7c8d9e0f1a2b.jsonl"),
				complete: `{"type":"assistant","message":{"content":[{"type":"text","text":"READY"}]}}`,
				torn:     `{"type":"assistant","message":{"content":[{"type":"te`,
			},
			{
				runner:   codex,
				handle:   Handle{Runner: "codex", Session: "codex-torn123", Cwd: cwd},
				log:      filepath.Join(codexDir, "codex-torn123.1.jsonl"),
				complete: `{"type":"thread","thread":{"messages":[{"role":"assistant","content":[{"type":"text","text":"READY"}]}]}}`,
				torn:     `{"type":"thread","thread":{"messages":[{"role":"assi`,
			},
		}
		for _, c := range cases {
			t.Run(c.runner.Name(), func(t *testing.T) {
				if err := os.WriteFile(c.log, []byte(c.complete+"\n"+c.torn), 0o644); err != nil {
					t.Fatalf("write log: %v", err)
				}
				got, err := c.runner.Transcript(c.handle)
				if err != nil {
					t.Fatalf("transcript mid-write: %v", err)
				}
				if !slices.Equal(got, []string{"READY"}) {
					t.Fatalf("transcript mid-write = %v, want [READY]", got)
				}
				if err := os.WriteFile(c.log, []byte(c.complete+"\n"+c.torn+"\n"), 0o644); err != nil {
					t.Fatalf("write log: %v", err)
				}
				if got, err := c.runner.Transcript(c.handle); err == nil {
					t.Fatalf("transcript with a malformed complete line = %v, want a parse error", got)
				}
			})
		}
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

	t.Run("A Runner Stops A Session And Keeps Its Conversation", func(t *testing.T) {
		t.Run("claude stops by its background id", func(t *testing.T) {
			h := Handle{Runner: "claude", Session: testSID, Ref: strPtr("b765c0a2"), Cwd: cwd}
			if err := claude.Stop(h); err != nil {
				t.Fatalf("claude stop: %v", err)
			}
			if !strings.Contains(calls(t, bin), "claude stop b765c0a2") {
				t.Fatalf("calls.log missing claude stop:\n%s", calls(t, bin))
			}
			if err := claude.Stop(Handle{Runner: "claude", Session: testSID, Cwd: cwd}); err == nil || !strings.Contains(err.Error(), "no background id") {
				t.Fatalf("claude stop with no ref = %v, want it refused naming the missing id", err)
			}
			failing := Handle{Runner: "claude", Session: testSID, Ref: strPtr("deadbeef"), Cwd: cwd}
			if err := claude.Stop(failing); err == nil || !strings.Contains(err.Error(), "no background session deadbeef") {
				t.Fatalf("claude stop failing = %v, want its stderr", err)
			}
		})
		t.Run("opencode and codex end the process serving the session", func(t *testing.T) {
			for _, r := range []Runner{opencode, codex} {
				proc := exec.Command("sleep", "30")
				if err := proc.Start(); err != nil {
					t.Fatalf("start: %v", err)
				}
				ended := make(chan error, 1)
				go func() { ended <- proc.Wait() }()
				h := Handle{Runner: r.Name(), Session: "s", Ref: pidRef(proc.Process.Pid), Cwd: cwd}
				if err := r.Stop(h); err != nil {
					t.Fatalf("%s stop: %v", r.Name(), err)
				}
				select {
				case <-ended:
				case <-time.After(5 * time.Second):
					proc.Process.Kill()
					t.Fatalf("%s stop left the process running", r.Name())
				}
				if err := r.Stop(h); err != nil {
					t.Fatalf("%s stop of a process already gone = %v, want nil", r.Name(), err)
				}
				if err := r.Stop(Handle{Runner: r.Name(), Session: "s", Cwd: cwd}); err != nil {
					t.Fatalf("%s stop with no process = %v, want nil", r.Name(), err)
				}
			}
		})
		t.Run("a runner file stops through its stop command, or says it has none", func(t *testing.T) {
			r, err := RunnerNamed("myagent")
			if err != nil {
				t.Fatalf("RunnerNamed: %v", err)
			}
			if err := r.Stop(Handle{Runner: "myagent", Session: "victory-001", Cwd: cwd}); err != nil {
				t.Fatalf("myagent stop: %v", err)
			}
			if !strings.Contains(calls(t, bin), "myagent stop victory-001") {
				t.Fatalf("calls.log missing myagent stop:\n%s", calls(t, bin))
			}
			bare, err := RunnerNamed("detached")
			if err != nil {
				t.Fatalf("RunnerNamed: %v", err)
			}
			err = bare.Stop(Handle{Runner: "detached", Session: "s", Cwd: cwd})
			if err == nil || !strings.Contains(err.Error(), "no stop command") || !strings.Contains(err.Error(), "detached.toml") {
				t.Fatalf("stop on a file with none = %v, want it naming the file", err)
			}
			if _, err := WriteSpec("badstop", "spawn = \"x {brief}\"\nsession_id = 's=(x)'\nstop = \"x stop {text}\"\n"); err == nil || !strings.Contains(err.Error(), "stop uses {text}") {
				t.Fatalf("a stop line using {text} = %v, want it rejected", err)
			}
		})
	})

	t.Run("A Runner Can Be Defined By A File Of Commands", func(t *testing.T) {
		t.Run("a ~/.work-director/runners/*.toml drives spawn, send, status, transcript, models", func(t *testing.T) {
			known, err := AllRunnerNames()
			if err != nil {
				t.Fatalf("allRunnerNames: %v", err)
			}
			slices.Sort(known)
			if !slices.Equal(known, []string{"claude", "codex", "detached", "myagent", "opencode"}) {
				t.Fatalf("runners = %v, want [claude codex detached myagent opencode]", known)
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
			if !strings.Contains(calls(t, bin), "myagent export victory-001 "+projectSlug(cwd)) {
				t.Fatalf("calls.log missing {slug_cwd} in transcript:\n%s", calls(t, bin))
			}
			dead := Handle{Runner: "myagent", Session: "dead", Cwd: cwd}
			if _, err := r.Status(dead); err == nil || !strings.Contains(err.Error(), "no such session") {
				t.Fatalf("status of a failing command = %v, want its stderr", err)
			}
			models, err := r.Models()
			if err != nil {
				t.Fatalf("myagent models: %v", err)
			}
			if !slices.Equal(models, []string{"victory/1"}) {
				t.Fatalf("models = %v, want [victory/1]", models)
			}
		})
		t.Run("a detached spec reads the session's log through {log}, from any process", func(t *testing.T) {
			r, err := RunnerNamed("detached")
			if err != nil {
				t.Fatalf("RunnerNamed: %v", err)
			}
			h, err := r.Spawn(SpawnOptions{Cwd: cwd, Name: "wd-9 t", Brief: "B"})
			if err != nil {
				t.Fatalf("detached spawn: %v", err)
			}
			fresh, err := RunnerNamed("detached")
			if err != nil {
				t.Fatalf("RunnerNamed: %v", err)
			}
			texts, err := fresh.Transcript(h)
			if err != nil {
				t.Fatalf("detached transcript: %v", err)
			}
			if !slices.Equal(texts, []string{"session=victory-001"}) {
				t.Fatalf("transcript = %v, want the spawn log", texts)
			}
		})
		t.Run("an unknown runner name fails loudly with guidance", func(t *testing.T) {
			_, err := RunnerNamed("cursor")
			if err == nil || !strings.Contains(err.Error(), "cursor") {
				t.Fatalf("err = %v, want a loud unknown-runner failure", err)
			}
		})
	})

	t.Run("A Runner File That Cannot Work Is Rejected When Added", func(t *testing.T) {
		base := `spawn = "x run {brief}"` + "\n" + `session_id = 'session=(\w+)'` + "\n"
		for _, c := range []struct{ name, text, want string }{
			{"badregex", base + `status = "x status {session}"` + "\n" + `running = '(unclosed'` + "\n", "running"},
			{"badplaceholder", base + `send = "x send {brief}"` + "\n", "{brief}"},
			{"badsession", `spawn = "x"` + "\n" + `session_id = '(bad'` + "\n", "session_id"},
		} {
			_, err := WriteSpec(c.name, c.text)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Errorf("%s: err = %v, want it to name %s", c.name, err, c.want)
			}
			if _, err := os.Stat(filepath.Join(specDir, c.name+".toml")); !os.IsNotExist(err) {
				t.Errorf("%s: written anyway (%v)", c.name, err)
			}
		}
	})
}

// ladderRunner makes two runners detected and gives each a fixed model list, so
// the ordering rules can be checked without putting fake CLIs on PATH.
func ladderRunner(name string, models ...Model) func(string) ([]Model, error) {
	return func(got string) ([]Model, error) {
		if got != name {
			return nil, fmt.Errorf("no runner called %s", got)
		}
		return models, nil
	}
}

func free(id string) Model { return Model{ID: id, Cost: Cost{}, CostKnown: true} }

func paid(id string, in, out float64) Model {
	return Model{ID: id, Cost: Cost{Input: in, Output: out}, CostKnown: true}
}

func unknown(id string) Model { return Model{ID: id} }

// A Role Takes The Cheapest Model That Also Does The Job
func TestARoleTakesTheCheapestModelThatAlsoDoesTheJob(t *testing.T) {
	both := []Availability{{Runner: "alpha", Detected: true}, {Runner: "beta", Detected: true}}

	t.Run("the cheapest that can do the job, not just the cheapest", func(t *testing.T) {
		// A free model that cannot hold the schema, and a paid one that can.
		// Ranking by price alone picks the first and promotes bad taste.
		probe := func(m Model, role Role) Verdict {
			if m.ID == "cheap-but-weak" {
				return Verdict{Probed: true, Why: "returned prose where the schema allows a label", Capable: false}
			}
			return Verdict{Probed: true, Capable: true, Why: "returned a valid verdict"}
		}
		got, err := Pick(RoleTaste, both, nil, probe, Policy{
			Role:   RolePolicy{},
			Lister: ladderRunner("alpha", free("cheap-but-weak"), paid("solid", 2, 10)),
		})
		if err != nil {
			t.Fatalf("Pick: %v", err)
		}
		if got.Model != "solid" {
			t.Errorf("chose %s, want solid: a free model that cannot do the job is not cheaper, it is wrong", got.Model)
		}
		if !got.Floor {
			t.Error("floor = false on a model that passed it")
		}
	})

	t.Run("probing stops at the first pass rather than ranking everything", func(t *testing.T) {
		asked := []string{}
		probe := func(m Model, role Role) Verdict {
			asked = append(asked, m.ID)
			return Verdict{Probed: true, Capable: m.ID == "second"}
		}
		_, err := Pick(RoleInterpret, both, nil, probe, Policy{
			Role:   RolePolicy{},
			Lister: ladderRunner("alpha", free("first"), free("second"), free("third"), free("fourth")),
		})
		if err != nil {
			t.Fatalf("Pick: %v", err)
		}
		if len(asked) != 2 || asked[0] != "first" || asked[1] != "second" {
			t.Errorf("probed %v, want it to stop at the second: probing all forty to rank them is how this stops being cheap", asked)
		}
	})

	t.Run("an unstated cost is unknown, not free", func(t *testing.T) {
		// unknown is not zero. Ranking it as zero is how a judgement starts
		// costing money without anyone having decided that it should.
		probe := func(m Model, role Role) Verdict { return Verdict{Probed: true, Capable: true} }
		got, err := Pick(RoleInterpret, both, nil, probe, Policy{
			Role:   RolePolicy{},
			Lister: ladderRunner("alpha", unknown("mystery"), paid("certain", 1, 4)),
		})
		if err != nil {
			t.Fatalf("Pick: %v", err)
		}
		if got.Model != "certain" {
			t.Errorf("chose %s, want certain: a model nobody stated a price for is unrankable", got.Model)
		}
	})

	t.Run("a model that could not be asked has not failed", func(t *testing.T) {
		// A quota or a missing session is us not knowing, which is not the same
		// as the model being incapable, and the ladder must not treat it as one.
		probe := func(m Model, role Role) Verdict {
			return Verdict{Unreachable: true, Why: "no session"}
		}
		_, err := Pick(RoleInterpret, both, nil, probe, Policy{
			Role:   RolePolicy{},
			Lister: ladderRunner("alpha", free("only")),
		})
		if err == nil || !strings.Contains(err.Error(), "no session") {
			t.Errorf("Pick = %v, want a refusal naming why, not a silent fallthrough", err)
		}
	})

	t.Run("every candidate tried is returned, so the ladder is inspectable", func(t *testing.T) {
		probe := func(m Model, role Role) Verdict { return Verdict{Probed: true, Why: "nope"} }
		_, err := Pick(RoleTaste, both, nil, probe, Policy{
			Role:   RolePolicy{},
			Lister: ladderRunner("alpha", free("a"), paid("b", 2, 10), paid("c", 1, 4)),
		})
		if err == nil {
			t.Fatal("expected a refusal when nothing meets the floor")
		}
		for _, id := range []string{"a", "b", "c"} {
			if !strings.Contains(err.Error(), id) {
				t.Errorf("refusal %q does not mention %s, so the ladder cannot be debugged", err, id)
			}
		}
	})

	t.Run("a preferred model that fails the floor is refused, not silently used", func(t *testing.T) {
		probe := func(m Model, role Role) Verdict {
			return Verdict{Probed: true, Capable: false, Why: "returned prose"}
		}
		_, err := Pick(RoleTaste, both, nil, probe, Policy{
			Role:   RolePolicy{Preferred: []string{"chosen"}},
			Lister: ladderRunner("alpha", paid("chosen", 0.5, 2), paid("other", 3, 15)),
		})
		if err == nil || !strings.Contains(err.Error(), "does not meet the floor") {
			t.Errorf("Pick = %v, want it refused naming the floor", err)
		}
	})
}

// A Runner That Reports Nothing Is Asked Rather Than Guessed At
func TestARunnerThatReportsNothingIsAskedRatherThanGuessedAt(t *testing.T) {
	both := []Availability{{Runner: "alpha", Detected: true}, {Runner: "quiet", Detected: true}}

	t.Run("with no floor data a runner is asked rather than one called capable", func(t *testing.T) {
		// A rankable model exists, but nothing checked whether it can do the
		// job. Naming it anyway would claim a floor nobody applied.
		got, err := Pick(RoleTaste, both, nil, nil, Policy{
			Role: RolePolicy{},
			Lister: func(r string) ([]Model, error) {
				if r == "quiet" {
					return nil, fmt.Errorf("models failed")
				}
				return []Model{free("a")}, nil
			},
		})
		if err != nil {
			t.Fatalf("Pick: %v", err)
		}
		if got.Model != "" {
			t.Errorf("model = %q, want none named: the floor was never checked on it", got.Model)
		}
		if !strings.Contains(got.Why, "was not checked") {
			t.Errorf("why = %q, want it to say the floor was not checked", got.Why)
		}
	})

	t.Run("no runner offers anything, so no model is named at all", func(t *testing.T) {
		got, err := Pick(RoleTaste, both, nil, nil, Policy{
			Role:   RolePolicy{},
			Lister: func(string) ([]Model, error) { return nil, nil },
		})
		if err != nil {
			t.Fatalf("Pick: %v", err)
		}
		if got.Model != "" {
			t.Errorf("model = %q, want none named: inventing an id is the registry this avoids", got.Model)
		}
		if got.Runner != "alpha" && got.Runner != "beta" && got.Runner != "quiet" {
			t.Errorf("runner = %q, want one of the detected runners asked to use its own default", got.Runner)
		}
		if !strings.Contains(got.Why, "one wd invented") {
			t.Errorf("why = %q, want it to say wd is not guessing", got.Why)
		}
	})

	t.Run("nothing detected at all is refused naming the command that reports it", func(t *testing.T) {
		_, err := Pick(RoleInterpret, nil, nil, nil, Policy{})
		if err == nil || !strings.Contains(err.Error(), "wd doctor") {
			t.Errorf("Pick = %v, want a refusal naming wd doctor", err)
		}
	})

	t.Run("declared costs are read from models.json, and an absent file is not an error", func(t *testing.T) {
		home := t.TempDir()
		costs, err := DeclaredCosts(home)
		if err != nil || len(costs) != 0 {
			t.Fatalf("DeclaredCosts with no file = %v, %v; want empty and no error", costs, err)
		}
		path := filepath.Join(home, "models.json")
		if err := os.WriteFile(path, []byte(`{"opencode/a":{"input":0,"output":0},"opencode/b":{"input":2,"output":10}}`), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
		costs, err = DeclaredCosts(home)
		if err != nil {
			t.Fatalf("DeclaredCosts: %v", err)
		}
		if costs["opencode/a"].Total() != 0 || costs["opencode/b"].Total() != 12 {
			t.Errorf("costs = %v, want the stated numbers", costs)
		}
		if err := os.WriteFile(path, []byte(`not json`), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
		if _, err := DeclaredCosts(home); err == nil {
			t.Error("a malformed models.json was accepted, want it refused naming the file")
		}
	})
}

// A Project Narrows The Policy And Can Never Widen It
func TestAProjectNarrowsThePolicyAndCanNeverWidenIt(t *testing.T) {
	global := RolePolicy{Runners: []string{"claude", "opencode"}}
	present := []string{"claude", "opencode", "codex"}

	t.Run("a project may forbid", func(t *testing.T) {
		got, err := Resolve(global, RolePolicy{Runners: []string{"claude"}}, RoleInterpret, present)
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		if len(got.Runners) != 1 || got.Runners[0] != "claude" {
			t.Errorf("runners = %v, want only claude", got.Runners)
		}
	})

	t.Run("a project may not permit, and is told what is", func(t *testing.T) {
		// codex is detected, which is exactly why this has to be refused: a
		// project that could re-allow a runner would make the global closure
		// one file away from undone.
		_, err := Resolve(global, RolePolicy{Runners: []string{"codex"}}, RoleInterpret, present)
		if err == nil || !strings.Contains(err.Error(), "never widen") {
			t.Fatalf("Resolve = %v, want it refused naming the asymmetry", err)
		}
		if !strings.Contains(err.Error(), "claude") {
			t.Errorf("refusal %q does not say what is allowed, so it is a dead end", err)
		}
	})

	t.Run("a forced model outside the allowed runners is refused", func(t *testing.T) {
		_, err := Resolve(global, RolePolicy{Forced: "codex/gpt-5.5"}, RoleInterpret, present)
		if err == nil || !strings.Contains(err.Error(), "not among the runners allowed") {
			t.Errorf("Resolve = %v, want it refused", err)
		}
		// The same model on an allowed runner is fine.
		got, err := Resolve(global, RolePolicy{Forced: "claude/claude-sonnet-5-5"}, RoleInterpret, present)
		if err != nil || got.Forced != "claude/claude-sonnet-5-5" {
			t.Errorf("Resolve = %+v, %v; want the force honoured", got, err)
		}
	})

	t.Run("a project inherits what it does not set", func(t *testing.T) {
		g := RolePolicy{Runners: []string{"claude"}, Preferred: []string{"claude-haiku-4-5"}}
		got, err := Resolve(g, RolePolicy{Runners: []string{"claude"}}, RoleTaste, present)
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		if len(got.Preferred) != 1 || got.Preferred[0] != "claude-haiku-4-5" {
			t.Errorf("preferred = %v, want the global preference inherited", got.Preferred)
		}
	})

	t.Run("promote_global defaults on and only ever narrows", func(t *testing.T) {
		on := true
		off := false

		// Unset on both sides means the default, which is on: global taste is
		// the director's own taste and travels by design.
		got, err := Resolve(RolePolicy{}, RolePolicy{}, RoleTaste, present)
		if err != nil || !PromotesGlobal(got) {
			t.Errorf("Resolve = %+v, %v; want promotion on by default", got, err)
		}
		got, err = Resolve(RolePolicy{PromoteGlobal: &on}, RolePolicy{}, RoleTaste, present)
		if err != nil || !PromotesGlobal(got) {
			t.Errorf("Resolve = %+v, %v; want the machine's yes inherited", got, err)
		}
		got, err = Resolve(RolePolicy{PromoteGlobal: &on}, RolePolicy{PromoteGlobal: &off}, RoleTaste, present)
		if err != nil || PromotesGlobal(got) {
			t.Errorf("Resolve = %+v, %v; want a project able to hold its cards still", got, err)
		}
		// The other direction is refused, for the same reason a project cannot
		// permit a runner: it may hold things still, not release them.
		_, err = Resolve(RolePolicy{PromoteGlobal: &off}, RolePolicy{PromoteGlobal: &on}, RoleTaste, present)
		if err == nil || !strings.Contains(err.Error(), "never widen") {
			t.Errorf("Resolve = %v, want a project's yes refused against a machine no", err)
		}
		// And unset is distinguishable from an explicit no, which is the whole
		// reason the field is a pointer.
		got, err = Resolve(RolePolicy{PromoteGlobal: &off}, RolePolicy{}, RoleTaste, present)
		if err != nil || PromotesGlobal(got) {
			t.Errorf("Resolve = %+v, %v; want an unset project to inherit the machine's no", got, err)
		}
	})
}

// A Model Outside The Allowed Runners Is Refused By Name
func TestAModelOutsideTheAllowedRunnersIsRefusedByName(t *testing.T) {
	open := RolePolicy{Runners: []string{"claude", "opencode"}}

	if err := open.Allowed("opencode/some-model"); err != nil {
		t.Errorf("Allowed(opencode model) = %v, want nil", err)
	}
	err := open.Allowed("codex/gpt-5.5")
	if err == nil || !strings.Contains(err.Error(), "not allowed here") {
		t.Fatalf("Allowed(codex model) = %v, want it refused", err)
	}
	if !strings.Contains(err.Error(), "claude") || !strings.Contains(err.Error(), "opencode") {
		t.Errorf("refusal %q does not name what is allowed", err)
	}

	// A forced role is the narrowest thing there is: anything else is refused
	// with the model that is in use, because otherwise a /model that appeared
	// to take would silently not.
	forced := RolePolicy{Forced: "claude/claude-sonnet-5-5"}
	if err := forced.Allowed("claude/claude-sonnet-5-5"); err != nil {
		t.Errorf("Allowed(the forced model) = %v, want nil", err)
	}
	err = forced.Allowed("claude/claude-opus-5-5")
	if err == nil || !strings.Contains(err.Error(), "is forced") {
		t.Errorf("Allowed(anything else) = %v, want it refused naming the forced model", err)
	}

	// No runners named means unrestricted, which is an unconfigured machine
	// rather than a closed one.
	if err := (RolePolicy{}).Allowed("codex/gpt-5.5"); err != nil {
		t.Errorf("Allowed with no policy = %v, want nil", err)
	}
}
