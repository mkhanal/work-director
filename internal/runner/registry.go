package runner

import (
	"fmt"
	"sort"
	"strings"
	"sync"
)

// The four foundation adapters are code: claude's two-step session resolve,
// opencode/codex staged session discovery and their transcript stores needed
// logic. Every *other* provider is a file of commands (see `wd runner init`).
var builtin = map[string]Runner{
	"claude":   claude,
	"opencode": opencode,
	"codex":    codex,
	"ao":       ao,
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
	dir := specDir()
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

// allRunnerNames lists the built-ins then every spec file's runner.
func allRunnerNames() ([]string, error) {
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

// runnerNamed resolves a built-in or spec-file runner; unknown names fail
// loudly with guidance.
func runnerNamed(name string) (Runner, error) {
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
