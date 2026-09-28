package runner

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// agentRow is one entry of `claude agents --json --all`.
type agentRow struct {
	ID        string `json:"id"`
	SessionID string `json:"sessionId"`
	Cwd       string `json:"cwd"`
	Status    string `json:"status"`
	State     string `json:"state"`
}

func agents(cwd string) ([]agentRow, error) {
	r, err := run([]string{"claude", "agents", "--json", "--all"}, cwd)
	if err != nil {
		return nil, err
	}
	if r.Code != 0 {
		return []agentRow{}, nil
	}
	var rows []agentRow
	if err := json.Unmarshal([]byte(r.Stdout), &rows); err != nil {
		return nil, err
	}
	return rows, nil
}

// claudeRefRe captures the background id from `claude --bg` output:
// "backgrounded · b765c0a2 · <name>".
var claudeRefRe = regexp.MustCompile(`backgrounded\s*·\s*([0-9a-f]{8})`)

type claudeRunner struct{}

// claude is the built-in claude adapter: spawn resolves the session id via
// `claude agents`, transcript reads the jsonl under ~/.claude/projects/<slug>.
var claude = claudeRunner{}

func (claudeRunner) Name() string { return "claude" }

func (claudeRunner) Spawn(o SpawnOptions) (Handle, error) {
	permissionMode := "bypassPermissions"
	if o.PermissionMode != nil {
		permissionMode = *o.PermissionMode
	}
	args := []string{"claude", "--bg", "-n", o.Name, "--permission-mode", permissionMode}
	if o.Model != nil {
		args = append(args, "--model", *o.Model)
	}
	if o.Worktree != nil && *o.Worktree {
		args = append(args, "--worktree", worktreeSlug(o.Name))
	}
	args = append(args, o.Brief)
	r, err := run(args, o.Cwd)
	if err != nil {
		return Handle{}, err
	}
	m := claudeRefRe.FindStringSubmatch(r.Stdout)
	if r.Code != 0 || m == nil {
		return Handle{}, &RunnerError{Runner: "claude", Detail: "spawn failed: " + stderrOrStdout(r)}
	}
	ref := m[1]
	row, ok := waitFor(func() (*agentRow, bool) {
		rows, err := agents(o.Cwd)
		if err != nil {
			return nil, false
		}
		for i, a := range rows {
			if a.ID == ref {
				return &rows[i], true
			}
		}
		return nil, false
	}, 15*time.Second, 500*time.Millisecond)
	if !ok {
		return Handle{}, &RunnerError{Runner: "claude", Detail: fmt.Sprintf("session %s not listed by claude agents", ref)}
	}
	return Handle{Runner: "claude", Session: row.SessionID, Ref: &ref, Cwd: row.Cwd}, nil
}

func (claudeRunner) Send(h *Handle, text string) error {
	r, err := run([]string{"claude", "--bg", "--resume", h.Session, text}, h.Cwd)
	if err != nil {
		return err
	}
	if r.Code != 0 {
		return &RunnerError{Runner: "claude", Detail: "send failed: " + stderrOrStdout(r)}
	}
	return nil
}

func (claudeRunner) Status(h Handle) (RunnerStatus, error) {
	rows, err := agents(h.Cwd)
	if err != nil {
		return "", err
	}
	for _, a := range rows {
		if a.SessionID != h.Session {
			continue
		}
		switch a.Status {
		case "waiting":
			return StatusWaiting, nil
		case "busy", "running":
			return StatusRunning, nil
		case "idle":
			return StatusIdle, nil
		}
		if a.State == "done" {
			return StatusIdle, nil
		}
		return StatusUnknown, nil
	}
	return StatusExited, nil
}

func (claudeRunner) Transcript(h Handle) ([]string, error) {
	file := filepath.Join(home(), ".claude", "projects", projectSlug(h.Cwd), h.Session+".jsonl")
	text, err := os.ReadFile(file)
	if err != nil {
		return []string{}, nil
	}
	var out []string
	for _, line := range strings.Split(string(text), "\n") {
		if line == "" {
			continue
		}
		var row struct {
			Type    string `json:"type"`
			Message struct {
				Content json.RawMessage `json:"content"`
			} `json:"message"`
		}
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			return nil, err
		}
		// Only assistant messages with an array content carry texts; a user
		// turn's content is a bare string.
		if row.Type != "assistant" || !bytes.HasPrefix(bytes.TrimSpace(row.Message.Content), []byte("[")) {
			continue
		}
		var content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}
		if err := json.Unmarshal(row.Message.Content, &content); err != nil {
			return nil, err
		}
		for _, part := range content {
			if part.Type == "text" && part.Text != "" {
				out = append(out, part.Text)
			}
		}
	}
	return out, nil
}

// claude's CLI has no non-interactive model list; its own /model picker is the list.
func (claudeRunner) Models() ([]string, error) { return []string{}, nil }

func (claudeRunner) AttachHint(h Handle) string {
	ref := h.Session
	if h.Ref != nil {
		ref = *h.Ref
	}
	return "claude attach " + ref
}
