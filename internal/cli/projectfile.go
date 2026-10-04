package cli

import (
	"os"
	"path/filepath"
	"strings"

	"wd/internal/project"
)

// csvFlags reads one flag as a comma-separated list, the shape every list flag
// in wd takes.
func csvFlags(a Args, key string) []string {
	v := str(a, key)
	if v == nil {
		return nil
	}
	out := []string{}
	for _, s := range strings.Split(*v, ",") {
		if t := strings.TrimSpace(s); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// registerProjectFile writes a project file and returns it parsed.
//
// Both `wd projects add` and `wd workspace create` go through it, so
// registering a project is one piece of behaviour with one set of checks rather
// than two that can disagree about what a project file has to survive: it has
// to read back exactly as it was written, or the director cannot trust it.
func (c *Cli) registerProjectFile(name, path string, opts project.NewProjectOptions) (*project.Project, error) {
	given := projectValues(path, opts)
	// The file is one `key: value` line per field.
	for _, g := range given {
		if strings.ContainsAny(g[1], "\r\n") {
			return nil, fail("%s %q cannot be held in a project file: it breaks the line", g[0], g[1])
		}
	}
	text := project.ProjectTemplate(name, path, opts)
	file := filepath.Join(c.ProjectsDir, name+".md")
	p, err := project.ParseProject(text, file)
	if err != nil {
		return nil, err
	}
	back := projectValues(p.Path, project.NewProjectOptions{
		Runner: p.Runner, Mode: string(p.Mode), Model: strOrEmpty(p.Model),
		Stack: p.Stack, Workflows: p.Workflows, Verify: p.Verify,
	})
	for i, g := range given {
		if g[1] != "" && (i >= len(back) || back[i] != g) {
			return nil, fail("%s %q cannot be held in a project file: it reads back changed", g[0], g[1])
		}
	}
	if err := os.WriteFile(file, []byte(text), 0o644); err != nil {
		return nil, err
	}
	return p, nil
}
