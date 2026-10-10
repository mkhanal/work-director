package runner

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// codexLogDir is where detached codex sessions write their jsonl stream.
func codexLogDir() (string, error) {
	wd, err := WDHome()
	if err != nil {
		return "", err
	}
	return filepath.Join(wd, "codex"), nil
}

// codexTurnLog names one turn's jsonl as <session>.<unix ms>.jsonl, so any
// process finds every turn of a session from its id alone. Session ids hold
// no dot (codexSessionIDRe).
func codexTurnLog(dir, session string, at time.Time) string {
	return filepath.Join(dir, fmt.Sprintf("%s.%d.jsonl", session, at.UnixMilli()))
}

// codexSessionIDRe captures the session id from codex's json stream.
var codexSessionIDRe = regexp.MustCompile(`"session_id"\s*:\s*"([A-Za-z0-9-]{10,})"`)

type codexRunner struct{}

// codex is the built-in codex adapter: detached `codex exec` with the session
// id discovered from the json stream, transcript from the recorded turn logs.
var codex = codexRunner{}

func (codexRunner) Name() string { return "codex" }

func (codexRunner) Command() string { return "codex" }

func (codexRunner) Spawn(o SpawnOptions) (Handle, error) {
	dir, err := codexLogDir()
	if err != nil {
		return Handle{}, err
	}
	started := time.Now()
	// The session id is only known once codex prints it, so the first turn
	// logs under a pending name and moves once the id is read.
	pending := filepath.Join(dir, fmt.Sprintf("spawn.%s.%d.jsonl", logName(o.Name), started.UnixMilli()))
	args := []string{"codex", "exec", "--cd", o.Cwd, "--json", "--full-auto"}
	if o.Model != nil {
		args = append(args, "--model", *o.Model)
	}
	args = append(args, o.Brief)
	pid, err := detach(args, o.Cwd, pending, dir)
	if err != nil {
		return Handle{}, err
	}
	session, ok, err := waitFor(func() (string, bool, error) { return firstCodexSessionId(pending) }, 60*time.Second, 500*time.Millisecond)
	if err != nil {
		return Handle{}, err
	}
	if !ok {
		return Handle{}, &RunnerError{Runner: "codex", Detail: fmt.Sprintf("no session_id in %s", pending)}
	}
	if err := moveLog(pending, codexTurnLog(dir, session, started)); err != nil {
		return Handle{}, err
	}
	return Handle{Runner: "codex", Session: session, Ref: pidRef(pid), Cwd: o.Cwd}, nil
}

func (codexRunner) Send(h *Handle, text string) error {
	dir, err := codexLogDir()
	if err != nil {
		return err
	}
	log := codexTurnLog(dir, h.Session, time.Now())
	pid, err := detach([]string{"codex", "exec", "--cd", h.Cwd, "--json", "resume", h.Session, text}, h.Cwd, log, dir)
	if err != nil {
		return err
	}
	h.Ref = pidRef(pid)
	return nil
}

// Stop ends the turn serving the session; codex keeps the session, and Send
// resumes it.
func (codexRunner) Stop(h Handle) error { return stopPid(h.Ref) }

func (codexRunner) Status(h Handle) (RunnerStatus, error) {
	if pid, ok := pidOf(h.Ref); ok && alive(pid) {
		return StatusRunning, nil
	}
	return StatusIdle, nil
}

// Transcript reads every turn log of the session, oldest turn first.
func (codexRunner) Transcript(h Handle) ([]string, error) {
	dir, err := codexLogDir()
	if err != nil {
		return nil, err
	}
	logs, err := codexTurnLogs(dir, h.Session)
	if err != nil {
		return nil, err
	}
	out := []string{}
	for _, log := range logs {
		texts, err := codexTexts(log)
		if err != nil {
			return nil, err
		}
		out = append(out, texts...)
	}
	return out, nil
}

// codexTurnLogs lists the session's turn logs ordered by their timestamp.
func codexTurnLogs(dir, session string) ([]string, error) {
	matches, err := filepath.Glob(filepath.Join(dir, session+".*.jsonl"))
	if err != nil {
		return nil, err
	}
	at := make(map[string]int64, len(matches))
	for _, m := range matches {
		stamp := strings.TrimSuffix(strings.TrimPrefix(filepath.Base(m), session+"."), ".jsonl")
		ms, err := strconv.ParseInt(stamp, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("codex turn log %s: %w", m, err)
		}
		at[m] = ms
	}
	sort.Slice(matches, func(i, j int) bool { return at[matches[i]] < at[matches[j]] })
	return matches, nil
}

// codexTexts reads one turn log's assistant texts and rollup summaries.
func codexTexts(log string) ([]string, error) {
	text, err := os.ReadFile(log)
	if err != nil {
		return nil, err
	}
	var out []string
	for i, line := range jsonlRecords(string(text)) {
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
			return nil, fmt.Errorf("%s:%d: %w", log, i+1, err)
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

func (codexRunner) Models() ([]string, error) {
	res, err := Run([]string{"codex", "debug", "models"}, "")
	if err != nil {
		return nil, err
	}
	if res.Code != 0 {
		return nil, &RunnerError{Runner: "codex", Detail: "models failed: " + res.Stderr}
	}
	return lines(res.Stdout), nil
}

func (codexRunner) AttachHint(h Handle) string {
	return "cd " + h.Cwd + " && codex exec --json resume " + h.Session
}

func firstCodexSessionId(log string) (string, bool, error) {
	text, err := os.ReadFile(log)
	if err != nil {
		return "", false, err
	}
	m := codexSessionIDRe.FindStringSubmatch(string(text))
	if m == nil {
		return "", false, nil
	}
	return m[1], true, nil
}
