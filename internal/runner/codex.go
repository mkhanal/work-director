package runner

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// codexLogDir is where detached codex sessions write their jsonl stream.
func codexLogDir() string {
	return filepath.Join(WDHome(), "codex")
}

// codexSessionIDRe captures the session id from codex's json stream.
var codexSessionIDRe = regexp.MustCompile(`"session_id"\s*:\s*"([A-Za-z0-9-]{10,})"`)

type codexRunner struct {
	// logs maps a session id to the jsonl file its events arrived in, matching
	// the TS module-level map: transcript reads the recorded stream.
	logs map[string]string
}

// codex is the built-in codex adapter: detached `codex exec` with the session
// id discovered from the json stream, transcript from the recorded log.
var codex = &codexRunner{logs: map[string]string{}}

func (r *codexRunner) Name() string { return "codex" }

func (r *codexRunner) Command() string { return "codex" }

func (r *codexRunner) Spawn(o SpawnOptions) (Handle, error) {
	log := filepath.Join(codexLogDir(), fmt.Sprintf("%s-%d.jsonl", logName(o.Name), time.Now().UnixMilli()))
	args := []string{"codex", "exec", "--cd", o.Cwd, "--json", "--full-auto"}
	if o.Model != nil {
		args = append(args, "--model", *o.Model)
	}
	args = append(args, o.Brief)
	pid, err := detach(args, o.Cwd, log, codexLogDir())
	if err != nil {
		return Handle{}, err
	}
	session, ok := waitFor(func() (string, bool) { return firstCodexSessionId(log) }, 60*time.Second, 500*time.Millisecond)
	if !ok {
		return Handle{}, &RunnerError{Runner: "codex", Detail: fmt.Sprintf("no session_id in %s", log)}
	}
	r.logs[session] = log
	return Handle{Runner: "codex", Session: session, Ref: pidRef(pid), Cwd: o.Cwd}, nil
}

func (r *codexRunner) Send(h *Handle, text string) error {
	log := filepath.Join(codexLogDir(), fmt.Sprintf("%s-%d.jsonl", h.Session, time.Now().UnixMilli()))
	pid, err := detach([]string{"codex", "exec", "--cd", h.Cwd, "--json", "resume", h.Session, text}, h.Cwd, log, codexLogDir())
	if err != nil {
		return err
	}
	r.logs[h.Session] = log
	h.Ref = pidRef(pid)
	return nil
}

func (r *codexRunner) Status(h Handle) (RunnerStatus, error) {
	if pid, ok := pidOf(h.Ref); ok && alive(pid) {
		return StatusRunning, nil
	}
	return StatusIdle, nil
}

func (r *codexRunner) Transcript(h Handle) ([]string, error) {
	log, ok := r.logs[h.Session]
	if !ok {
		return []string{}, nil
	}
	text, err := os.ReadFile(log)
	if err != nil {
		return []string{}, nil
	}
	var out []string
	for _, line := range strings.Split(string(text), "\n") {
		if line == "" {
			continue
		}
		var evt struct {
			Thread struct {
				Messages []struct {
					Role    string `json:"role"`
					Content []struct {
						Type string `json:"type"`
						Text string `json:"text"`
					} `json:"content"`
				} `json:"messages"`
			} `json:"thread"`
			Rollup struct {
				Summary string `json:"summary"`
			} `json:"rollup"`
		}
		if err := json.Unmarshal([]byte(line), &evt); err != nil {
			continue
		}
		for _, m := range evt.Thread.Messages {
			if m.Role != "assistant" {
				continue
			}
			for _, p := range m.Content {
				if p.Type == "text" && p.Text != "" {
					out = append(out, p.Text)
				}
			}
		}
		if evt.Rollup.Summary != "" {
			out = append(out, evt.Rollup.Summary)
		}
	}
	return out, nil
}

func (r *codexRunner) Models() ([]string, error) {
	res, err := Run([]string{"codex", "debug", "models"}, "")
	if err != nil {
		return nil, err
	}
	if res.Code != 0 {
		return nil, &RunnerError{Runner: "codex", Detail: "models failed: " + res.Stderr}
	}
	return lines(res.Stdout), nil
}

func (r *codexRunner) AttachHint(h Handle) string {
	return "cd " + h.Cwd + " && codex exec --json resume " + h.Session
}

func firstCodexSessionId(log string) (string, bool) {
	text, err := os.ReadFile(log)
	if err != nil {
		return "", false
	}
	m := codexSessionIDRe.FindStringSubmatch(string(text))
	if m == nil {
		return "", false
	}
	return m[1], true
}
