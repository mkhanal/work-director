package runner

import (
	"encoding/json"
	"os"
	"regexp"
	"strings"
)

// harness maps agent names to ao harness flags (`ao spawn --harness`).
var harness = map[string]string{"claude": "claude-code", "codex": "codex", "opencode": "opencode"}

// sessionIdFromRe captures the session id from `ao spawn` output:
// "spawned session scratch-1 (idle) [prompt 58 B, system 7883 B]".
var sessionIdFromRe = regexp.MustCompile(`spawned session (\S+)`)

type aoRunner struct{}

// ao is the built-in Agent Orchestrator adapter: the brief is the prompt,
// follow-ups go via `ao send --session/--message`.
var ao = aoRunner{}

func (aoRunner) Name() string { return "ao" }

func (aoRunner) Command() string { return "ao" }

func (aoRunner) Spawn(o SpawnOptions) (Handle, error) {
	if o.Model != nil {
		return Handle{}, &RunnerError{Runner: "ao", Detail: "model " + *o.Model + ": ao spawn has no --model flag; set the model on the ao project"}
	}
	args := []string{"ao", "spawn", "--name", name20(o.Name), "--prompt", o.Brief}
	if o.Agent != nil {
		h := *o.Agent
		if mapped, ok := harness[*o.Agent]; ok {
			h = mapped
		}
		args = append(args, "--harness", h)
	}
	if projectID := os.Getenv("AO_PROJECT_ID"); projectID != "" {
		args = append(args, "--project", projectID)
	}
	r, err := Run(args, o.Cwd)
	if err != nil {
		return Handle{}, err
	}
	var session string
	if r.Code == 0 {
		if m := sessionIdFromRe.FindStringSubmatch(strings.TrimSpace(r.Stdout)); m != nil {
			session = m[1]
		}
	}
	if session == "" {
		return Handle{}, &RunnerError{Runner: "ao", Detail: "spawn failed: " + stderrOrStdout(r)}
	}
	return Handle{Runner: "ao", Session: session, Cwd: o.Cwd}, nil
}

func (aoRunner) Send(h *Handle, text string) error {
	r, err := Run([]string{"ao", "send", "--session", h.Session, "--message", text}, h.Cwd)
	if err != nil {
		return err
	}
	if r.Code != 0 {
		return &RunnerError{Runner: "ao", Detail: "send failed: " + stderrOrStdout(r)}
	}
	return nil
}

func (aoRunner) Status(h Handle) (RunnerStatus, error) {
	r, err := Run([]string{"ao", "session", "get", h.Session, "--json"}, h.Cwd)
	if err != nil {
		return "", err
	}
	if r.Code != 0 {
		return StatusUnknown, nil
	}
	// `ao session get --json` → { session: { activity: { state }, isTerminated, status } }
	var j struct {
		Session struct {
			Activity struct {
				State string `json:"state"`
			} `json:"activity"`
			IsTerminated bool `json:"isTerminated"`
		} `json:"session"`
	}
	if err := json.Unmarshal([]byte(r.Stdout), &j); err != nil {
		return "", err
	}
	if j.Session.IsTerminated {
		return StatusExited, nil
	}
	switch j.Session.Activity.State {
	case "active":
		return StatusRunning, nil
	case "idle":
		return StatusIdle, nil
	case "waiting_input", "blocked":
		return StatusWaiting, nil
	default:
		return StatusUnknown, nil
	}
}

func (aoRunner) Transcript(h Handle) ([]string, error) { return []string{}, nil }

func (aoRunner) Models() ([]string, error) { return []string{}, nil }

func (aoRunner) AttachHint(h Handle) string {
	return "Agent Orchestrator app → session " + h.Session + " (ao session get " + h.Session + " --json)"
}
