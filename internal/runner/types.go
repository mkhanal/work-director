// Package runner drives provider CLIs: the three foundation adapters (claude,
// opencode, codex) and spec-file runners defined by TOML command files.
package runner

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Handle is a live executor session: which runner, which session id, an
// optional process ref for liveness, and the working directory.
type Handle struct {
	Runner  string  `json:"runner"`
	Session string  `json:"session"`
	Ref     *string `json:"ref"`
	Cwd     string  `json:"cwd"`
}

// SpawnOptions is everything a runner needs to start a session. Optional
// fields are pointers: unset means the runner's own default.
type SpawnOptions struct {
	Cwd            string
	Name           string
	Brief          string
	Agent          *string
	PermissionMode *string
	Worktree       *bool
	Model          *string
}

// RunnerStatus is the director's view of a session's liveness.
type RunnerStatus string

const (
	StatusRunning RunnerStatus = "running"
	StatusIdle    RunnerStatus = "idle"
	StatusWaiting RunnerStatus = "waiting"
	StatusExited  RunnerStatus = "exited"
	StatusUnknown RunnerStatus = "unknown"
)

// Runner drives one provider's CLI, the executable Command names. Send takes a pointer because detached
// runners (opencode, codex, spec) record the new process ref on the handle.
type Runner interface {
	Name() string
	Command() string
	Spawn(o SpawnOptions) (Handle, error)
	Send(h *Handle, text string) error
	Status(h Handle) (RunnerStatus, error)
	Transcript(h Handle) ([]string, error)
	Models() ([]string, error)
	AttachHint(h Handle) string
}

// RunnerError names the runner and what failed; the runner is the boundary.
type RunnerError struct {
	Runner string
	Detail string
}

func (e *RunnerError) Error() string { return e.Runner + ": " + e.Detail }

// RunResult is one command run: exit code and captured output.
type RunResult struct {
	Code   int
	Stdout string
	Stderr string
}

// exe resolves name against the current PATH.
func exe(name string) (string, error) {
	found, err := exec.LookPath(name)
	if err != nil {
		return "", fmt.Errorf("executable not found: %s", name)
	}
	return found, nil
}

// Run executes cmd in cwd, capturing stdout and stderr. A non-zero exit is a
// result, not an error; only spawn failures of the process itself error.
func Run(cmd []string, cwd string) (RunResult, error) {
	path, err := exe(cmd[0])
	if err != nil {
		return RunResult{}, err
	}
	c := exec.Command(path, cmd[1:]...)
	c.Dir = cwd
	var stdout, stderr bytes.Buffer
	c.Stdout = &stdout
	c.Stderr = &stderr
	err = c.Run()
	res := RunResult{Stdout: stdout.String(), Stderr: stderr.String()}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		res.Code = exitErr.ExitCode()
		return res, nil
	}
	if err != nil {
		return RunResult{}, err
	}
	return res, nil
}

// waitFor polls probe every `every` until it returns ok or the deadline
// passes; the zero value and false mean "never appeared".
func waitFor[T any](probe func() (T, bool), timeout time.Duration, every time.Duration) (T, bool) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if v, ok := probe(); ok {
			return v, true
		}
		time.Sleep(every)
	}
	var zero T
	return zero, false
}

// alive reports whether the process exists (signal 0 probes without killing).
func alive(pid int) bool {
	return syscall.Kill(pid, 0) == nil
}

// pidOf parses a handle's string ref; a missing or non-numeric ref is not a
// live process (the TS Number(h.ref) is NaN then, and alive(NaN) is false).
func pidOf(ref *string) (int, bool) {
	if ref == nil {
		return 0, false
	}
	pid, err := strconv.Atoi(*ref)
	if err != nil {
		return 0, false
	}
	return pid, true
}

func pidRef(pid int) *string {
	s := strconv.Itoa(pid)
	return &s
}

// stderrOrStdout matches the TS `r.stderr || r.stdout` error detail.
func stderrOrStdout(r RunResult) string {
	if r.Stderr != "" {
		return r.Stderr
	}
	return r.Stdout
}

// lines splits command output into trimmed non-empty lines.
func lines(text string) []string {
	var out []string
	for _, s := range strings.Split(text, "\n") {
		if t := strings.TrimSpace(s); t != "" {
			out = append(out, t)
		}
	}
	return out
}

var slugRe = regexp.MustCompile(`[^A-Za-z0-9-]`)

// projectSlug matches claude's project dir naming: cwd with every
// non-[A-Za-z0-9-] character as '-'.
func projectSlug(cwd string) string {
	return slugRe.ReplaceAllString(cwd, "-")
}

var logNameRe = regexp.MustCompile(`[^A-Za-z0-9-]+`)

// logName sanitizes a display name for a log file name: runs of
// non-[A-Za-z0-9-] become a single '-'.
func logName(s string) string {
	return logNameRe.ReplaceAllString(s, "-")
}

// name20 truncates a display name to 20 characters (the {name20} spec placeholder).
func name20(s string) string {
	if len(s) > 20 {
		return s[:20]
	}
	return s
}

var worktreeSlugRe = regexp.MustCompile(`[^a-z0-9]+`)

// worktreeSlug matches claude's --worktree naming: lowercased name with runs
// of non-[a-z0-9] collapsed to a single '-'.
func worktreeSlug(s string) string {
	return worktreeSlugRe.ReplaceAllString(strings.ToLower(s), "-")
}
