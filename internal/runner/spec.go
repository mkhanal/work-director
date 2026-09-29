package runner

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// SpecDir is where runner spec files live: ~/.work-director/runners.
func SpecDir() string {
	return filepath.Join(WDHome(), "runners")
}

// RunnerSpec is one runner file: the commands that drive a provider's CLI.
// Placeholders {cwd} {name} {brief} {model} {agent} {session} {text} {home}
// {slug_cwd} {log} {name20} are shell-quoted automatically.
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

// loadSpecs reads every *.toml in dir; a file's name field wins, else the
// file name (without .toml) is the runner name.
func loadSpecs(dir string) ([]RunnerSpec, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return []RunnerSpec{}, nil
	}
	var out []RunnerSpec
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
			return nil, fmt.Errorf("%s: %v", e.Name(), err)
		}
		if spec.Name == "" {
			spec.Name = strings.TrimSuffix(e.Name(), ".toml")
		}
		out = append(out, spec)
	}
	return out, nil
}

// WriteSpec validates a spec file's text and writes it under SpecDir as
// <name>.toml, forcing the name field to the file's name.
func WriteSpec(name, text string) (string, error) {
	spec, err := parseSpecTOML(text)
	if err != nil {
		return "", err
	}
	if spec.Spawn == "" {
		return "", &RunnerError{Runner: name, Detail: "spawn command is required"}
	}
	if spec.SessionID == "" {
		return "", &RunnerError{Runner: name, Detail: "session_id regex is required (capture the session id in spawn output)"}
	}
	if err := os.MkdirAll(SpecDir(), 0o755); err != nil {
		return "", err
	}
	file := filepath.Join(SpecDir(), name+".toml")
	replaced := regexp.MustCompile(`(?m)^name\s*=.*$`).ReplaceAllString(text, `name = "`+name+`"`)
	if err := os.WriteFile(file, []byte(replaced), 0o644); err != nil {
		return "", err
	}
	return file, nil
}

// SpecTemplate is the starter file `wd runner init` writes.
func SpecTemplate(name string) string {
	return `# ` + name + ` — a runner is one file of commands against a provider's CLI.
# Placeholders are shell-quoted automatically: {cwd} {name} {brief} {model} {agent}
# {session} {text} {home} {slug_cwd} {log} {name20}. Write commands exactly as you
# would run them, including pipes and redirects.
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

// fill replaces {word} placeholders with shell-quoted values; unknown words
// fill empty.
func fill(tpl string, v map[string]string) string {
	return placeholderRe.ReplaceAllStringFunc(tpl, func(m string) string {
		key := m[1 : len(m)-1]
		if value, ok := v[key]; ok {
			return q(value)
		}
		return ""
	})
}

// firstId returns the first regex match: capture group 1 when the pattern has
// one, else the whole match.
func firstId(text, re string) (string, bool) {
	m, err := regexp.Compile(re)
	if err != nil {
		return "", false
	}
	loc := m.FindStringSubmatch(text)
	if loc == nil {
		return "", false
	}
	if m.NumSubexp() > 0 {
		return loc[1], true
	}
	return loc[0], true
}

func readFileOrEmpty(path string) string {
	text, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(text)
}

// shell runs a spec command line through bash -lc in cwd.
func shell(cmd, cwd string) (RunResult, error) {
	return Run([]string{"bash", "-lc", cmd}, cwd)
}

// specRunner adapts a RunnerSpec to the Runner interface.
func specRunner(spec RunnerSpec) Runner {
	return &specRunnerAdapter{spec: spec}
}

type specRunnerAdapter struct {
	spec RunnerSpec
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

// placeholders is the map every spec command fills; only the fields the
// command uses are non-empty.
func (r *specRunnerAdapter) placeholders(h Handle, text string) map[string]string {
	return map[string]string{
		"cwd": h.Cwd, "name": "", "name20": "", "brief": "", "model": "", "agent": "",
		"home": home(), "slug_cwd": "", "session": h.Session, "text": text, "log": "",
	}
}

func (r *specRunnerAdapter) Spawn(o SpawnOptions) (Handle, error) {
	if r.spec.Spawn == "" || r.spec.SessionID == "" {
		return Handle{}, &RunnerError{Runner: r.spec.Name, Detail: "spec needs spawn and session_id"}
	}
	if o.Model != nil && !strings.Contains(r.spec.Spawn, "{model}") {
		return Handle{}, &RunnerError{Runner: r.spec.Name, Detail: "model " + *o.Model + ": spawn line has no {model} placeholder"}
	}
	common := map[string]string{
		"cwd": o.Cwd, "name": o.Name, "name20": name20(o.Name), "brief": o.Brief,
		"model": strOrEmpty(o.Model), "agent": strOrEmpty(o.Agent), "home": home(),
		"slug_cwd": projectSlug(o.Cwd), "session": "", "text": "", "log": "",
	}
	cmd := fill(r.spec.Spawn, common)
	if r.spec.Detach {
		log := filepath.Join(SpecDir(), fmt.Sprintf("%s-%d.log", r.spec.Name, time.Now().UnixMilli()))
		pid, err := detach([]string{"bash", "-lc", cmd}, o.Cwd, log, SpecDir())
		if err != nil {
			return Handle{}, err
		}
		session, ok := waitFor(func() (string, bool) {
			return firstId(readFileOrEmpty(log), r.spec.SessionID)
		}, 60*time.Second, 500*time.Millisecond)
		if !ok {
			return Handle{}, &RunnerError{Runner: r.spec.Name, Detail: fmt.Sprintf("no session id in %s", log)}
		}
		return Handle{Runner: r.spec.Name, Session: session, Ref: pidRef(pid), Cwd: o.Cwd}, nil
	}
	res, err := shell(cmd, o.Cwd)
	if err != nil {
		return Handle{}, err
	}
	session, ok := firstId(res.Stdout+res.Stderr, r.spec.SessionID)
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
	if r.spec.Status != "" {
		res, err := shell(fill(r.spec.Status, r.placeholders(h, "")), h.Cwd)
		if err != nil {
			return "", err
		}
		if res.Code != 0 {
			return StatusUnknown, nil
		}
		body := res.Stdout
		for _, class := range []struct {
			re     string
			status RunnerStatus
		}{
			{r.spec.Exited, StatusExited},
			{r.spec.Waiting, StatusWaiting},
			{r.spec.Running, StatusRunning},
		} {
			if class.re == "" {
				continue
			}
			compiled, err := regexp.Compile(class.re)
			if err != nil {
				return "", &RunnerError{Runner: r.spec.Name, Detail: "status regex " + class.re + ": " + err.Error()}
			}
			if compiled.MatchString(body) {
				return class.status, nil
			}
		}
		return StatusIdle, nil
	}
	if pid, ok := pidOf(h.Ref); ok && alive(pid) {
		return StatusRunning, nil
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
	res, err := shell(fill(r.spec.Models, r.placeholders(Handle{}, "")), "")
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
