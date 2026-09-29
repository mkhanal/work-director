package runner

import (
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"sync"
)

// The three foundation adapters are code: claude's two-step session resolve,
// opencode/codex staged session discovery and their transcript stores needed
// logic. Every *other* provider is a file of commands (see `wd runner init`).
var builtin = map[string]Runner{
	"claude":   claude,
	"opencode": opencode,
	"codex":    codex,
}

// IsBuiltin reports whether name is one of the foundation adapters rather
// than a spec file.
func IsBuiltin(name string) bool {
	_, ok := builtin[name]
	return ok
}

func builtinNames() []string {
	out := make([]string, 0, len(builtin))
	for name := range builtin {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// specsCache memoizes the spec list per directory, so parallel runners with
// different WD_HOME values do not collide.
type specsCache struct {
	at   string
	list []RunnerSpec
}

var (
	cacheMu sync.Mutex
	cache   *specsCache
)

func specs() ([]RunnerSpec, error) {
	dir := SpecDir()
	cacheMu.Lock()
	defer cacheMu.Unlock()
	if cache == nil || cache.at != dir {
		list, err := loadSpecs(dir)
		if err != nil {
			return nil, err
		}
		cache = &specsCache{at: dir, list: list}
	}
	return cache.list, nil
}

// AllRunnerNames lists the built-ins then every spec file's runner.
func AllRunnerNames() ([]string, error) {
	ss, err := specs()
	if err != nil {
		return nil, err
	}
	out := builtinNames()
	for _, s := range ss {
		out = append(out, s.Name)
	}
	return out, nil
}

// RunnerNamed resolves a built-in or spec-file runner; unknown names fail
// loudly with guidance.
func RunnerNamed(name string) (Runner, error) {
	if b, ok := builtin[name]; ok {
		return b, nil
	}
	ss, err := specs()
	if err != nil {
		return nil, err
	}
	for _, s := range ss {
		if s.Name == name {
			return specRunner(s), nil
		}
	}
	return nil, fmt.Errorf("no runner named %q; known built-ins: %s. Add ~/.work-director/runners/%s.toml (see wd runner init)",
		name, strings.Join(builtinNames(), ", "), name)
}

// NotDetectedError is a registered runner whose command is not on PATH, so
// nothing can be spawned to it on this host.
type NotDetectedError struct {
	Runner  string
	Command string
}

func (e *NotDetectedError) Error() string {
	return fmt.Sprintf("runner %s is not detected: %q not found on PATH. Install it, or add a provider that is not built in with wd runner init <name>; wd doctor lists what is detected",
		e.Runner, e.Command)
}

// DetectedRunner resolves a runner to spawn to: registered and with its
// command on PATH, else a NotDetectedError.
func DetectedRunner(name string) (Runner, error) {
	r, err := RunnerNamed(name)
	if err != nil {
		return nil, err
	}
	a := availability(r)
	if !a.Detected {
		return nil, &NotDetectedError{Runner: a.Runner, Command: a.Command}
	}
	return r, nil
}

// Availability is whether one registered runner can be used on this host:
// detected when its command resolves on PATH, with the resolved path.
type Availability struct {
	Runner   string  `json:"runner"`
	Command  string  `json:"command"`
	Detected bool    `json:"detected"`
	Path     *string `json:"path"`
}

// Availabilities reports every registered runner, built-ins then spec
// files. An undetected runner is a fact about the host, not an error.
func Availabilities() ([]Availability, error) {
	names, err := AllRunnerNames()
	if err != nil {
		return nil, err
	}
	out := make([]Availability, 0, len(names))
	for _, name := range names {
		r, err := RunnerNamed(name)
		if err != nil {
			return nil, err
		}
		out = append(out, availability(r))
	}
	return out, nil
}

func availability(r Runner) Availability {
	a := Availability{Runner: r.Name(), Command: r.Command()}
	if a.Command == "" {
		return a
	}
	path, err := exec.LookPath(a.Command)
	if err != nil {
		return a
	}
	a.Detected = true
	a.Path = &path
	return a
}
