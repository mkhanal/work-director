package project

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func knownRunners() ([]string, error) { return []string{"claude", "opencode", "codex"}, nil }

func TestProject(t *testing.T) {
	t.Run("Parse Project Reads The Frontmatter Fields", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		text := "---\npath: ~/repo\nrunner: claude\nstack: [go, ts]\nworkflows: [/lazyspec]\nverify: [go test]\n---\nThe roadmap body.\n"
		p, err := ParseProject(text, filepath.Join(home, "proj.md"))
		if err != nil {
			t.Fatalf("parse project: %v", err)
		}
		if p.Name != "proj" {
			t.Fatalf("name = %q, want proj", p.Name)
		}
		if want := filepath.Join(home, "repo"); p.Path != want {
			t.Fatalf("path = %q, want %q", p.Path, want)
		}
		if p.Runner != "claude" || p.Mode != ModeAuto {
			t.Fatalf("runner/mode = %q/%q, want claude/auto", p.Runner, p.Mode)
		}
		if !slices.Equal(p.Stack, []string{"go", "ts"}) || !slices.Equal(p.Workflows, []string{"/lazyspec"}) || !slices.Equal(p.Verify, []string{"go test"}) {
			t.Fatalf("lists = %v/%v/%v", p.Stack, p.Workflows, p.Verify)
		}
		if p.InstructionsFile != "AGENTS.md" || p.DefaultBranch != "main" {
			t.Fatalf("defaults = %q/%q, want AGENTS.md/main", p.InstructionsFile, p.DefaultBranch)
		}
		if p.Roadmap != "The roadmap body." {
			t.Fatalf("roadmap = %q, want the body", p.Roadmap)
		}
	})

	t.Run("A Missing Field Fails Naming The File And Field", func(t *testing.T) {
		_, err := ParseProject("---\nrunner: claude\n---\n", "/home/proj.md")
		pe, ok := err.(*ProjectError)
		if !ok || pe.Path != "/home/proj.md" || pe.Detail != "missing field path" {
			t.Fatalf("err = %v, want ProjectError /home/proj.md: missing field path", err)
		}
		_, err = ParseProject("no frontmatter", "/home/proj.md")
		pe, ok = err.(*ProjectError)
		if !ok || pe.Detail != "missing frontmatter" {
			t.Fatalf("err = %v, want ProjectError missing frontmatter", err)
		}
	})

	t.Run("An Unknown Mode Fails", func(t *testing.T) {
		_, err := ParseProject("---\npath: /repo\nrunner: claude\nmode: sometimes\n---\n", "/home/proj.md")
		pe, ok := err.(*ProjectError)
		if !ok || pe.Detail != "unknown mode sometimes" {
			t.Fatalf("err = %v, want ProjectError unknown mode sometimes", err)
		}
	})

	t.Run("The Project Template Writes Concrete Frontmatter", func(t *testing.T) {
		got := ProjectTemplate("proj", "/repo", NewProjectOptions{})
		want := `---
path: /repo
runner: claude
mode: auto
model: 
stack: []
workflows: []
verify: []
instructions_file: AGENTS.md
default_branch: main
---
Roadmap, most important first. The director reads this file; it never edits the repo it describes.
`
		if got != want {
			t.Fatalf("template =\n%q\nwant\n%q", got, want)
		}
	})

	t.Run("An Empty Runner List Falls Back To The Project Runner", func(t *testing.T) {
		for _, value := range []string{"", " , "} {
			got, err := ParseRunnerList(value, "claude", knownRunners)
			if err != nil {
				t.Fatalf("parse runner list %q: %v", value, err)
			}
			if !slices.Equal(got, []string{"claude"}) {
				t.Fatalf("runners = %v, want [claude]", got)
			}
		}
	})

	t.Run("An Unknown Runner Fails Loudly", func(t *testing.T) {
		_, err := ParseRunnerList("cursor", "claude", knownRunners)
		pe, ok := err.(*ProjectError)
		if !ok || pe.Path != "--runner" || !strings.Contains(pe.Detail, "cursor") || !strings.Contains(pe.Detail, "known:") {
			t.Fatalf("err = %v, want ProjectError naming cursor and the known runners", err)
		}
	})

	t.Run("Install Lazyspec Creates The Evolution Work Item", func(t *testing.T) {
		title, detail := InstallLazyspec("Install lazyspec")
		if title != "Install lazyspec" {
			t.Fatalf("title = %q, want the given title", title)
		}
		for _, want := range []string{"lazyspec.md", "## ", "married"} {
			if !strings.Contains(detail, want) {
				t.Fatalf("detail missing %q: %s", want, detail)
			}
		}
	})

	t.Run("Load Projects Skips The Readme", func(t *testing.T) {
		dir := t.TempDir()
		files := map[string]string{
			"proj.md":   "---\npath: /repo\nrunner: claude\n---\n",
			"other.md":  "---\npath: /repo2\nrunner: opencode\n---\n",
			"README.md": "---\npath: /nope\nrunner: claude\n---\n",
			"notes.txt": "---\npath: /nope\nrunner: claude\n---\n",
		}
		for name, text := range files {
			if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o644); err != nil {
				t.Fatalf("write %s: %v", name, err)
			}
		}
		projects, err := LoadProjects(dir)
		if err != nil {
			t.Fatalf("load projects: %v", err)
		}
		if len(projects) != 2 || projects["proj"] == nil || projects["other"] == nil {
			t.Fatalf("projects = %v, want proj and other only", projects)
		}
	})
}
