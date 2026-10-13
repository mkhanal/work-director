package serve

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"wd/internal/core"
	"wd/internal/ledger"
	"wd/internal/runner"
)

// idleClaude is the claude runner reading real stores, with every session
// idle: a benchmark lists no claude processes.
type idleClaude struct {
	runner.Runner
	runner.Inspector
}

func (idleClaude) Status(runner.Handle) (runner.RunnerStatus, error) { return runner.StatusIdle, nil }

// BenchmarkGoalSwitch reads a person's goals in turn, as switching between
// them in an app does: three goals of fifteen tasks, each task with a claude
// session of a few hundred steps and its subagents, about two thousand events,
// worked in one git repository.
func BenchmarkGoalSwitch(b *testing.B) {
	home := b.TempDir()
	b.Setenv("HOME", home)
	projects := filepath.Join(home, ".claude", "projects")
	for i := range 40 {
		if err := os.MkdirAll(filepath.Join(projects, fmt.Sprintf("-tmp-other-%02d", i)), 0o755); err != nil {
			b.Fatal(err)
		}
	}
	store := filepath.Join(projects, "-tmp-repo")
	repo := b.TempDir()
	for _, args := range [][]string{
		{"init", "-q", "-b", "main"},
		{"-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "--allow-empty", "-m", "init"},
		{"switch", "-q", "-c", "goal-branch"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			b.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	l, err := ledger.New(filepath.Join(b.TempDir(), "ledger.db"))
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { l.Close() })
	claude, err := runner.RunnerNamed("claude")
	if err != nil {
		b.Fatal(err)
	}
	r := idleClaude{claude, claude.(runner.Inspector)}
	s := New(l, "", func(w core.Work) (runner.Runner, runner.Handle, bool, error) {
		if w.Session == nil {
			return nil, runner.Handle{}, false, nil
		}
		return r, runner.Handle{Runner: "claude", Session: *w.Session, Cwd: repo}, true, nil
	}, nil)

	var goals []string
	for g := range 3 {
		goal, err := l.Add("p", fmt.Sprintf("Goal %d", g), ledger.AddOptions{Kind: core.WorkGoal})
		if err != nil {
			b.Fatal(err)
		}
		goals = append(goals, goal.ID)
		for t := range 15 {
			task, err := l.Add("p", fmt.Sprintf("Task %d.%d", g, t), ledger.AddOptions{Parent: &goal.ID})
			if err != nil {
				b.Fatal(err)
			}
			session := fmt.Sprintf("00000000-0000-0000-%04d-%012d", g, t)
			if err := l.SetSession(task.ID, ledger.SessionInfo{Runner: "claude", Session: session, Cwd: repo}); err != nil {
				b.Fatal(err)
			}
			writeStore(b, filepath.Join(store, session+".jsonl"), 300, false)
			for a := range 2 {
				writeStore(b, filepath.Join(store, session, "subagents", fmt.Sprintf("agent-%d.jsonl", a)), 40, true)
			}
			for e := range 40 {
				kind, body := core.EventNote, fmt.Sprintf("note %d on the way", e)
				if e%10 == 9 {
					kind, body = core.EventReport, "DONE: it works\nFILES: a.go b.go\nNOTES:\n"+strings.Repeat("what was found and why. ", 20)
				}
				if _, err := l.AddEvent(task.ID, kind, body); err != nil {
					b.Fatal(err)
				}
			}
		}
	}

	b.ResetTimer()
	for i := range b.N {
		if _, err := s.goal(goals[i%len(goals)]); err != nil {
			b.Fatal(err)
		}
	}
}

// writeStore writes a claude store of steps prompts, each answered with a
// tool call, its result and a reply.
func writeStore(b *testing.B, file string, steps int, sidechain bool) {
	b.Helper()
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		b.Fatal(err)
	}
	var sb strings.Builder
	side := ""
	if sidechain {
		side = `"isSidechain":true,`
	}
	for i := range steps {
		at := fmt.Sprintf(`"timestamp":"2026-10-11T10:%02d:%02dZ",`, i/60%60, i%60)
		fmt.Fprintf(&sb, `{"type":"user",%s%s"message":{"content":"step %d: look at the code"}}`+"\n", at, side, i)
		fmt.Fprintf(&sb, `{"type":"assistant",%s%s"message":{"model":"claude-x","usage":{"input_tokens":10,"output_tokens":20},"content":[{"type":"tool_use","id":"t%d","name":"Bash","input":{"command":"ls -la"}}]}}`+"\n", at, side, i)
		fmt.Fprintf(&sb, `{"type":"user",%s%s"message":{"content":[{"type":"tool_result","tool_use_id":"t%d","content":%q}]}}`+"\n", at, side, i, strings.Repeat("file.go\n", 30))
		fmt.Fprintf(&sb, `{"type":"assistant",%s%s"message":{"model":"claude-x","stop_reason":"end_turn","usage":{"input_tokens":10,"output_tokens":20},"content":[{"type":"text","text":"Read it; %s"}]}}`+"\n", at, side, strings.Repeat("this is what it says. ", 10))
	}
	if err := os.WriteFile(file, []byte(sb.String()), 0o644); err != nil {
		b.Fatal(err)
	}
}
