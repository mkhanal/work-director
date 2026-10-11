package runner

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"time"
)

// logDir is where detached opencode sessions write their jsonl stream.
func logDir() (string, error) {
	wd, err := WDHome()
	if err != nil {
		return "", err
	}
	return filepath.Join(wd, "opencode"), nil
}

// detach starts args with stdout/stderr in log files under dir and returns its
// pid; the process outlives the director (Go needs no unref for that).
func detach(args []string, cwd, log, dir string) (int, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return 0, err
	}
	path, err := exe(args[0])
	if err != nil {
		return 0, err
	}
	out, err := os.Create(log)
	if err != nil {
		return 0, err
	}
	defer out.Close()
	errOut, err := os.Create(log + ".err")
	if err != nil {
		return 0, err
	}
	defer errOut.Close()
	c := exec.Command(path, args[1:]...)
	c.Dir = cwd
	c.Stdout = out
	c.Stderr = errOut
	if err := c.Start(); err != nil {
		return 0, err
	}
	return c.Process.Pid, nil
}

// moveLog renames a detached process's log pair while it may still be
// writing: the open descriptors follow the rename.
func moveLog(from, to string) error {
	if err := os.Rename(from, to); err != nil {
		return err
	}
	return os.Rename(from+".err", to+".err")
}

// sessionIDRe captures the session id from opencode's json stream.
var sessionIDRe = regexp.MustCompile(`"sessionID":"(ses_[A-Za-z0-9]+)"`)

func firstSessionId(log string) (string, bool, error) {
	text, err := os.ReadFile(log)
	if err != nil {
		return "", false, err
	}
	m := sessionIDRe.FindStringSubmatch(string(text))
	if m == nil {
		return "", false, nil
	}
	return m[1], true, nil
}

type opencodeRunner struct{}

// opencode is the built-in opencode adapter: detached `opencode run` with the
// session id discovered from the json stream, transcript via `session export`.
var opencode = opencodeRunner{}

func (opencodeRunner) Name() string { return "opencode" }

func (opencodeRunner) Command() string { return "opencode" }

func (opencodeRunner) Spawn(o SpawnOptions) (Handle, error) {
	dir, err := logDir()
	if err != nil {
		return Handle{}, err
	}
	log := filepath.Join(dir, fmt.Sprintf("%s-%d.jsonl", logName(o.Name), time.Now().UnixMilli()))
	args := []string{"opencode", "run", "--format", "json", "--auto", "--title", o.Name}
	if o.Model != nil {
		args = append(args, "--model", *o.Model)
	}
	if o.Agent != nil {
		args = append(args, "--agent", *o.Agent)
	}
	args = append(args, o.Brief)
	pid, err := detach(args, o.Cwd, log, dir)
	if err != nil {
		return Handle{}, err
	}
	session, ok, err := waitFor(func() (string, bool, error) { return firstSessionId(log) }, 60*time.Second, 500*time.Millisecond)
	if err != nil {
		return Handle{}, err
	}
	if !ok {
		return Handle{}, &RunnerError{Runner: "opencode", Detail: fmt.Sprintf("no sessionID in %s", log)}
	}
	return Handle{Runner: "opencode", Session: session, Ref: pidRef(pid), Cwd: o.Cwd}, nil
}

func (opencodeRunner) Send(h *Handle, text string) error {
	dir, err := logDir()
	if err != nil {
		return err
	}
	log := filepath.Join(dir, fmt.Sprintf("%s-%d.jsonl", h.Session, time.Now().UnixMilli()))
	pid, err := detach([]string{"opencode", "run", "--format", "json", "--auto", "-s", h.Session, text}, h.Cwd, log, dir)
	if err != nil {
		return err
	}
	h.Ref = pidRef(pid)
	return nil
}

// Stop ends the run serving the session; opencode keeps the session, and Send
// continues it with -s.
func (opencodeRunner) Stop(h Handle) error { return stopPid(h.Ref) }

func (opencodeRunner) Status(h Handle) (RunnerStatus, error) {
	if pid, ok := pidOf(h.Ref); ok && alive(pid) {
		return StatusRunning, nil
	}
	return StatusIdle, nil
}

// Conversation is what the session said: opencode's export is read for its
// assistant texts.
func (opencodeRunner) Conversation(h Handle) ([]Entry, error) {
	r, err := Run([]string{"opencode", "session", "export", h.Session}, h.Cwd)
	if err != nil {
		return nil, err
	}
	if r.Code != 0 {
		return nil, &RunnerError{Runner: "opencode", Detail: "export failed: " + r.Stderr}
	}
	var data struct {
		Messages []struct {
			Type    string `json:"type"`
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal([]byte(r.Stdout), &data); err != nil {
		return nil, err
	}
	var out []string
	for _, m := range data.Messages {
		if m.Type != "assistant" {
			continue
		}
		for _, p := range m.Content {
			if p.Type == "text" && p.Text != "" {
				out = append(out, p.Text)
			}
		}
	}
	return texts(out), nil
}

func (opencodeRunner) Models() ([]string, error) {
	r, err := Run([]string{"opencode", "models"}, "")
	if err != nil {
		return nil, err
	}
	if r.Code != 0 {
		return nil, &RunnerError{Runner: "opencode", Detail: "models failed: " + r.Stderr}
	}
	return lines(r.Stdout), nil
}

func (opencodeRunner) AttachHint(h Handle) string {
	return "cd " + h.Cwd + " && opencode -s " + h.Session
}
