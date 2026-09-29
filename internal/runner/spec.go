package runner

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
)

// SpecDir is where runner spec files live: ~/.work-director/runners.
func SpecDir() (string, error) {
	wd, err := WDHome()
	if err != nil {
		return "", err
	}
	return filepath.Join(wd, "runners"), nil
}

// RunnerSpec is one runner file: the commands that drive a provider's CLI.
// Placeholders are shell-quoted automatically; which ones a command may use
// is commandPlaceholders.
type RunnerSpec struct {
	Name       string
	Spawn      string
	Detach     bool
	SessionID  string
	Send       string
	Status     string
	Running    string
	Waiting    string
	Exited     string
	Transcript string
	Models     string
	Attach     string
}

// checkedSpec is a RunnerSpec that can run: required commands present,
// regexes compiled, every placeholder one its command can fill, and the
// values fixed at load resolved.
type checkedSpec struct {
	RunnerSpec
	sessionID *regexp.Regexp
	// running, waiting and exited are nil when the spec leaves them out.
	running, waiting, exited *regexp.Regexp
	// home is resolved only when a command uses {home}.
	home string
	// logDir holds a detached spawn's output, one file per session.
	logDir string
}

// specCommand is one command line of a spec and the placeholders it can fill.
type specCommand struct {
	key     string
	line    string
	allowed []string
}

// commandPlaceholders lists each command with the placeholders it can fill. A
// session's {log} exists only when spawn is detached.
func commandPlaceholders(s RunnerSpec) []specCommand {
	session := []string{"cwd", "session", "home", "slug_cwd"}
	if s.Detach {
		session = append(session, "log")
	}
	return []specCommand{
		{"spawn", s.Spawn, []string{"cwd", "name", "name20", "brief", "model", "agent", "home", "slug_cwd"}},
		{"send", s.Send, append(slices.Clone(session), "text")},
		{"status", s.Status, session},
		{"transcript", s.Transcript, session},
		{"attach", s.Attach, session},
		{"models", s.Models, []string{"home"}},
	}
}

// checkSpec validates a parsed spec whose files live in dir.
func checkSpec(s RunnerSpec, dir string) (checkedSpec, error) {
	fail := func(detail string) (checkedSpec, error) {
		return checkedSpec{}, &RunnerError{Runner: s.Name, Detail: detail}
	}
	if s.Spawn == "" {
		return fail("spawn command is required")
	}
	if s.SessionID == "" {
		return fail("session_id regex is required (capture the session id in spawn output)")
	}
	c := checkedSpec{RunnerSpec: s, logDir: filepath.Join(dir, "logs", s.Name)}
	for _, re := range []struct {
		key  string
		text string
		dst  **regexp.Regexp
	}{
		{"session_id", s.SessionID, &c.sessionID},
		{"running", s.Running, &c.running},
		{"waiting", s.Waiting, &c.waiting},
		{"exited", s.Exited, &c.exited},
	} {
		if re.text == "" {
			continue
		}
		compiled, err := regexp.Compile(re.text)
		if err != nil {
			return fail(re.key + " regex: " + err.Error())
		}
		*re.dst = compiled
	}
	usesHome := false
	for _, cmd := range commandPlaceholders(s) {
		for _, m := range placeholderRe.FindAllStringSubmatch(cmd.line, -1) {
			key := m[1]
			if !slices.Contains(cmd.allowed, key) {
				if key == "log" {
					return fail(cmd.key + " uses {log}, which only a detach = true spec has, and never in spawn")
				}
				return fail(fmt.Sprintf("%s uses {%s}; it can use {%s}", cmd.key, key, strings.Join(cmd.allowed, "} {")))
			}
			usesHome = usesHome || key == "home"
		}
	}
	if usesHome {
		home, err := os.UserHomeDir()
		if err != nil {
			return fail("{home}: " + err.Error())
		}
		c.home = home
	}
	return c, nil
}

// loadSpecs reads every *.toml in dir; a file's name field wins, else the
// file name (without .toml) is the runner name. A missing dir holds none.
func loadSpecs(dir string) ([]checkedSpec, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return []checkedSpec{}, nil
	}
	if err != nil {
		return nil, err
	}
	var out []checkedSpec
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".toml") {
			continue
		}
		text, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		spec, err := parseSpecTOML(string(text))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", e.Name(), err)
		}
		if spec.Name == "" {
			spec.Name = strings.TrimSuffix(e.Name(), ".toml")
		}
		checked, err := checkSpec(spec, dir)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", e.Name(), err)
		}
		out = append(out, checked)
	}
	return out, nil
}

// WriteSpec validates a spec file's text and writes it under the spec dir as
// <name>.toml, forcing the name field to the file's name.
func WriteSpec(name, text string) (string, error) {
	spec, err := parseSpecTOML(text)
	if err != nil {
		return "", err
	}
	spec.Name = name
	dir, err := SpecDir()
	if err != nil {
		return "", err
	}
	if _, err := checkSpec(spec, dir); err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	file := filepath.Join(dir, name+".toml")
	replaced := regexp.MustCompile(`(?m)^name\s*=.*$`).ReplaceAllString(text, `name = "`+name+`"`)
	if err := os.WriteFile(file, []byte(replaced), 0o644); err != nil {
		return "", err
	}
	return file, nil
}

// SpecTemplate is the starter file `wd runner init` writes.
func SpecTemplate(name string) string {
	return `# ` + name + ` — a runner is one file of commands against a provider's CLI.
# Placeholders are shell-quoted automatically. spawn: {cwd} {name} {name20}
# {brief} {model} {agent} {home} {slug_cwd}. send, status, transcript, attach:
# {cwd} {session} {home} {slug_cwd}, send also {text}, and {log} (the session's
# spawn output) when detach = true. models: {home}. Write commands exactly as
# you would run them, including pipes and redirects.
name = "` + name + `"
spawn = "` + name + ` run --cwd {cwd} --title {name} --json {brief}"
detach = true
session_id = '"session":"([A-Za-z0-9_-]+)"'
send = "` + name + ` send --session {session} {text}"
running = 'state: *running'
waiting = 'state: *waiting'
exited = 'state: *exited'
transcript = "` + name + ` export --session {session}"
models = "` + name + ` models"
attach = "cd {cwd} && ` + name + ` -s {session}"

# status: omit to report liveness only; include to classify output by these regexes
# (exited, then waiting, then running). transcript/models return one item per line.
# session_id is required: the first match in spawn output names the session.
# A spawn line without {model} refuses a spawn that names a model.
`
}

// q shell-quotes s for `bash -lc`.
func q(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

var placeholderRe = regexp.MustCompile(`\{(\w+)\}`)

// fill replaces {word} placeholders with shell-quoted values; checkSpec
// guarantees v holds every placeholder the command uses.
func fill(tpl string, v map[string]string) string {
	return placeholderRe.ReplaceAllStringFunc(tpl, func(m string) string {
		return q(v[m[1:len(m)-1]])
	})
}

// firstId returns the first regex match: capture group 1 when the pattern has
// one, else the whole match.
func firstId(text string, re *regexp.Regexp) (string, bool) {
	loc := re.FindStringSubmatch(text)
	if loc == nil {
		return "", false
	}
	if re.NumSubexp() > 0 {
		return loc[1], true
	}
	return loc[0], true
}

// shell runs a spec command line through bash -lc in cwd.
func shell(cmd, cwd string) (RunResult, error) {
	return Run([]string{"bash", "-lc", cmd}, cwd)
}

// specRunner adapts a checked spec to the Runner interface.
func specRunner(spec checkedSpec) Runner {
	return &specRunnerAdapter{spec: spec}
}

type specRunnerAdapter struct {
	spec checkedSpec
}

func (r *specRunnerAdapter) Name() string { return r.spec.Name }

// Command is the first word of the spawn line: a spec names its executable
// there, before any arguments.
func (r *specRunnerAdapter) Command() string {
	fields := strings.Fields(r.spec.Spawn)
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}

// sessionLog is the file a detached spawn's output lands in for session.
func (r *specRunnerAdapter) sessionLog(session string) string {
	return filepath.Join(r.spec.logDir, logName(session)+".log")
}

// placeholders fills every command that addresses a session.
func (r *specRunnerAdapter) placeholders(h Handle, text string) map[string]string {
	return map[string]string{
		"cwd": h.Cwd, "session": h.Session, "text": text, "home": r.spec.home,
		"slug_cwd": projectSlug(h.Cwd), "log": r.sessionLog(h.Session),
	}
}

func (r *specRunnerAdapter) Spawn(o SpawnOptions) (Handle, error) {
	if o.Model != nil && !strings.Contains(r.spec.Spawn, "{model}") {
		return Handle{}, &RunnerError{Runner: r.spec.Name, Detail: "model " + *o.Model + ": spawn line has no {model} placeholder"}
	}
	cmd := fill(r.spec.Spawn, map[string]string{
		"cwd": o.Cwd, "name": o.Name, "name20": name20(o.Name), "brief": o.Brief,
		"model": strOrEmpty(o.Model), "agent": strOrEmpty(o.Agent), "home": r.spec.home,
		"slug_cwd": projectSlug(o.Cwd),
	})
	if r.spec.Detach {
		// logName leaves no dot, so the pending name never meets a session's.
		pending := filepath.Join(r.spec.logDir, fmt.Sprintf("spawn.%d.log", time.Now().UnixMilli()))
		pid, err := detach([]string{"bash", "-lc", cmd}, o.Cwd, pending, r.spec.logDir)
		if err != nil {
			return Handle{}, err
		}
		session, ok, err := waitFor(func() (string, bool, error) {
			text, err := os.ReadFile(pending)
			if err != nil {
				return "", false, err
			}
			id, ok := firstId(string(text), r.spec.sessionID)
			return id, ok, nil
		}, 60*time.Second, 500*time.Millisecond)
		if err != nil {
			return Handle{}, err
		}
		if !ok {
			return Handle{}, &RunnerError{Runner: r.spec.Name, Detail: fmt.Sprintf("no session id in %s", pending)}
		}
		if err := moveLog(pending, r.sessionLog(session)); err != nil {
			return Handle{}, err
		}
		return Handle{Runner: r.spec.Name, Session: session, Ref: pidRef(pid), Cwd: o.Cwd}, nil
	}
	res, err := shell(cmd, o.Cwd)
	if err != nil {
		return Handle{}, err
	}
	session, ok := firstId(res.Stdout+res.Stderr, r.spec.sessionID)
	if !ok {
		return Handle{}, &RunnerError{Runner: r.spec.Name, Detail: fmt.Sprintf("no session id in spawn output: %s", stderrOrStdout(res))}
	}
	return Handle{Runner: r.spec.Name, Session: session, Cwd: o.Cwd}, nil
}

func (r *specRunnerAdapter) Send(h *Handle, text string) error {
	if r.spec.Send == "" {
		return &RunnerError{Runner: r.spec.Name, Detail: "no send command in spec"}
	}
	res, err := shell(fill(r.spec.Send, r.placeholders(*h, text)), h.Cwd)
	if err != nil {
		return err
	}
	if res.Code != 0 {
		return &RunnerError{Runner: r.spec.Name, Detail: "send failed: " + res.Stderr}
	}
	return nil
}

func (r *specRunnerAdapter) Status(h Handle) (RunnerStatus, error) {
	if r.spec.Status == "" {
		if pid, ok := pidOf(h.Ref); ok && alive(pid) {
			return StatusRunning, nil
		}
		return StatusIdle, nil
	}
	res, err := shell(fill(r.spec.Status, r.placeholders(h, "")), h.Cwd)
	if err != nil {
		return "", err
	}
	if res.Code != 0 {
		return "", &RunnerError{Runner: r.spec.Name, Detail: "status failed: " + stderrOrStdout(res)}
	}
	for _, class := range []struct {
		re     *regexp.Regexp
		status RunnerStatus
	}{
		{r.spec.exited, StatusExited},
		{r.spec.waiting, StatusWaiting},
		{r.spec.running, StatusRunning},
	} {
		if class.re != nil && class.re.MatchString(res.Stdout) {
			return class.status, nil
		}
	}
	return StatusIdle, nil
}

func (r *specRunnerAdapter) Transcript(h Handle) ([]string, error) {
	if r.spec.Transcript == "" {
		return []string{}, nil
	}
	res, err := shell(fill(r.spec.Transcript, r.placeholders(h, "")), h.Cwd)
	if err != nil {
		return nil, err
	}
	if res.Code != 0 {
		return nil, &RunnerError{Runner: r.spec.Name, Detail: "transcript failed: " + res.Stderr}
	}
	return lines(res.Stdout), nil
}

func (r *specRunnerAdapter) Models() ([]string, error) {
	if r.spec.Models == "" {
		return []string{}, nil
	}
	res, err := shell(fill(r.spec.Models, map[string]string{"home": r.spec.home}), "")
	if err != nil {
		return nil, err
	}
	if res.Code != 0 {
		return nil, &RunnerError{Runner: r.spec.Name, Detail: "models failed: " + res.Stderr}
	}
	return lines(res.Stdout), nil
}

func (r *specRunnerAdapter) AttachHint(h Handle) string {
	if r.spec.Attach != "" {
		return fill(r.spec.Attach, r.placeholders(h, ""))
	}
	return r.spec.Name + " session " + h.Session
}

func strOrEmpty(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
