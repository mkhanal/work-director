// Package project parses and renders the director's per-project files under
// ~/.work-director: frontmatter fields, the template `wd projects add`
// writes, and the runner-list flag.
package project

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"wd/internal/taste"
)

// Mode is how much the director asks before acting.
type Mode string

const (
	ModeAsk  Mode = "ask"
	ModeAuto Mode = "auto"
)

// Project is one managed repo: where it is, which runner drives it, and how
// it is verified.
type Project struct {
	Name             string   `json:"name"`
	Path             string   `json:"path"`
	Runner           string   `json:"runner"`
	Agent            *string  `json:"agent,omitempty"`
	Model            *string  `json:"model,omitempty"`
	Mode             Mode     `json:"mode"`
	Stack            []string `json:"stack"`
	Workflows        []string `json:"workflows"`
	Verify           []string `json:"verify"`
	InstructionsFile string   `json:"instructionsFile"`
	DefaultBranch    string   `json:"defaultBranch"`
	Roadmap          string   `json:"roadmap"`
	// Policy restricts which runners each role may reach for this project, keyed
	// by role name. It narrows the machine's global policy and never widens it:
	// a project that forbids a provider cannot re-allow it here, or closing it
	// globally would be one project file away from undone.
	Policy map[string]string `json:"policy,omitempty"`
}

// RestrictedRunners is what the policy says for one role, as a list. An empty
// result means the role is unrestricted here, which is the default.
func (p *Project) RestrictedRunners(role string) []string {
	v, ok := p.Policy[role]
	if !ok {
		return nil
	}
	return taste.List(v)
}

// ProjectError names the file and what is wrong with it.
// parsePolicy reads the optional policy field: a comma-separated list of
// runner:role pairs, one per restriction, of the form
//
//	interpret=claude, taste=claude
//
// It is one scalar rather than a nested block because that is what the
// frontmatter parser holds, and because the rule a project states is short: this
// project does not let these roles reach anything but that runner. Absence means
// no restriction, which is what an unconfigured project has.
func parsePolicy(v string) map[string]string {
	out := map[string]string{}
	for _, part := range taste.List(v) {
		role, runners, ok := strings.Cut(part, "=")
		if !ok || strings.TrimSpace(role) == "" || strings.TrimSpace(runners) == "" {
			continue
		}
		out[strings.TrimSpace(role)] = strings.TrimSpace(runners)
	}
	return out
}

// ProjectError names the file and what is wrong with it.
type ProjectError struct {
	Path   string
	Detail string
}

func (e *ProjectError) Error() string { return e.Path + ": " + e.Detail }

// ParseProject reads one project file's frontmatter. Every field except
// mode, agent and model is required; mode defaults to auto.
func ParseProject(text, path string) (*Project, error) {
	fm := taste.ParseFrontmatter(text)
	if fm == nil {
		return nil, &ProjectError{Path: path, Detail: "missing frontmatter"}
	}
	fields := fm.Fields
	need := func(k string) (string, error) {
		v, ok := fields[k]
		if !ok || v == "" {
			return "", &ProjectError{Path: path, Detail: "missing field " + k}
		}
		return v, nil
	}
	runner, err := need("runner")
	if err != nil {
		return nil, err
	}
	pathField, err := need("path")
	if err != nil {
		return nil, err
	}
	mode := Mode(fields["mode"])
	if mode == "" {
		mode = ModeAuto
	}
	if mode != ModeAsk && mode != ModeAuto {
		return nil, &ProjectError{Path: path, Detail: "unknown mode " + string(mode)}
	}
	agent := fields["agent"]
	model := fields["model"]
	repo, err := ExpandHome(pathField)
	if err != nil {
		return nil, &ProjectError{Path: path, Detail: err.Error()}
	}
	p := &Project{
		Policy:           parsePolicy(fields["policy"]),
		Name:             strings.TrimSuffix(filepath.Base(path), ".md"),
		Path:             repo,
		Runner:           runner,
		Mode:             mode,
		Stack:            taste.List(fields["stack"]),
		Workflows:        taste.List(fields["workflows"]),
		Verify:           taste.List(fields["verify"]),
		InstructionsFile: fields["instructions_file"],
		DefaultBranch:    fields["default_branch"],
		Roadmap:          fm.Body,
	}
	if p.InstructionsFile == "" {
		p.InstructionsFile = "AGENTS.md"
	}
	if p.DefaultBranch == "" {
		p.DefaultBranch = "main"
	}
	if agent != "" {
		p.Agent = &agent
	}
	if model != "" {
		p.Model = &model
	}
	return p, nil
}

// ExpandHome expands a leading ~ or ~/ to the user's home directory, and
// fails when that directory cannot be resolved; ~user paths stay as given.
func ExpandHome(path string) (string, error) {
	if path != "~" && !strings.HasPrefix(path, "~/") {
		return path, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("expand %s: %w", path, err)
	}
	return home + path[1:], nil
}

// ProjectTemplate is the file `wd projects add` writes: concrete frontmatter,
// no lazyspec claim (presence lives in the repo it manages).
func ProjectTemplate(name, path string, o NewProjectOptions) string {
	listv := func(v []string) string { return "[" + strings.Join(v, ", ") + "]" }
	return fmt.Sprintf(`---
path: %s
runner: %s
mode: %s
model: %s
stack: %s
workflows: %s
verify: %s
instructions_file: %s
default_branch: %s
---
%s
`, path, orDefault(o.Runner, "claude"), orDefault(o.Mode, "auto"), orDefault(o.Model, ""),
		listv(orDefaultSlice(o.Stack)), listv(orDefaultSlice(o.Workflows)), listv(orDefaultSlice(o.Verify)),
		orDefault(o.InstructionsFile, "AGENTS.md"), orDefault(o.DefaultBranch, "main"),
		orDefault(o.Roadmap, "Roadmap, most important first. The director reads this file; it never edits the repo it describes."))
}

// NewProjectOptions carries the optional fields of ProjectTemplate; unset
// means the default.
type NewProjectOptions struct {
	Runner           string
	Mode             string
	Model            string
	Stack            []string
	Workflows        []string
	Verify           []string
	InstructionsFile string
	DefaultBranch    string
	Roadmap          string
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

func orDefaultSlice(v []string) []string {
	if v == nil {
		return []string{}
	}
	return v
}

// ParseRunnerList parcels sessions across providers: `--runner
// claude,opencode,myagent`. An empty list falls back to the project's
// runner; unknown names fail loud.
func ParseRunnerList(value, fallback string, known func() ([]string, error)) ([]string, error) {
	var list []string
	for _, s := range strings.Split(value, ",") {
		if t := strings.TrimSpace(s); t != "" {
			list = append(list, t)
		}
	}
	chosen := list
	if len(chosen) == 0 {
		chosen = []string{fallback}
	}
	names, err := known()
	if err != nil {
		return nil, err
	}
	for _, r := range chosen {
		if !slices.Contains(names, r) {
			return nil, &ProjectError{Path: "--runner", Detail: "unknown runner " + r + "; known: " + strings.Join(names, ", ")}
		}
	}
	return chosen, nil
}

// InstallLazyspec is the work item "yes" creates: install the director's
// preferred lazyspec in a project, as an evolution PR.
func InstallLazyspec(title string) (string, string) {
	detail := "Install the director's preferred lazyspec in this repo: requirements live in `*.lazyspec.md`, each `## ` heading married to a test that repeats the heading word for word, changed together through the repo's own `/lazyspec`. Add the convention to the repo's agent files (instructions file, workflows), never to work-director files."
	return title, detail
}

// LoadProjects reads every project file in dir except README.md, keyed by
// name.
func LoadProjects(dir string) (map[string]*Project, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	out := map[string]*Project{}
	for _, f := range entries {
		if !strings.HasSuffix(f.Name(), ".md") || f.Name() == "README.md" {
			continue
		}
		text, err := os.ReadFile(filepath.Join(dir, f.Name()))
		if err != nil {
			return nil, err
		}
		p, err := ParseProject(string(text), filepath.Join(dir, f.Name()))
		if err != nil {
			return nil, err
		}
		out[p.Name] = p
	}
	return out, nil
}
