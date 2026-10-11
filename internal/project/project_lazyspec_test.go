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

	t.Run("A Tilde Path Fails When The Home Directory Is Unknown", func(t *testing.T) {
		t.Setenv("HOME", "")
		for _, raw := range []string{"~", "~/repo"} {
			p, err := ParseProject("---\npath: "+raw+"\nrunner: claude\n---\n", "/home/proj.md")
			pe, ok := err.(*ProjectError)
			if !ok || pe.Path != "/home/proj.md" {
				t.Fatalf("path %q: project = %+v, err = %v, want ProjectError naming /home/proj.md", raw, p, err)
			}
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

// repo writes files into a fresh directory and returns it.
func repo(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, text := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestProjectToolkit(t *testing.T) {
	t.Run("A Repository's Toolkit Gives Its Checks", func(t *testing.T) {
		scripts := `{"scripts":{"test":"x","validate":"x","lint":"x","build":"x"}}`
		for lock, manager := range map[string]string{"pnpm-lock.yaml": "pnpm", "yarn.lock": "yarn", "bun.lock": "bun", "bun.lockb": "bun", "package-lock.json": "npm", "": "npm"} {
			files := map[string]string{"package.json": scripts}
			if lock != "" {
				files[lock] = ""
			}
			kit, err := ReadToolkit(repo(t, files))
			if err != nil {
				t.Fatal(err)
			}
			want := []string{manager + " run lint", manager + " run validate", manager + " run test"}
			if kit.PackageManager != manager || !slices.Equal(kit.Checks, want) {
				t.Fatalf("lock %q: kit = %+v, want %s with %q", lock, kit, manager, want)
			}
		}
		kit, err := ReadToolkit(repo(t, map[string]string{"go.mod": "module x", "Cargo.toml": ""}))
		if err != nil {
			t.Fatal(err)
		}
		if want := []string{"go vet ./...", "go test ./...", "cargo test"}; kit.PackageManager != "" || !slices.Equal(kit.Checks, want) {
			t.Fatalf("go and cargo kit = %+v, want %q", kit, want)
		}
		kit, err = ReadToolkit(repo(t, map[string]string{"README.md": "hi"}))
		if err != nil || len(kit.Checks) != 0 {
			t.Fatalf("bare repo kit = %+v, %v, want no checks", kit, err)
		}
	})

	t.Run("A Repository's Toolkit Names The Specifications It Keeps", func(t *testing.T) {
		kit, err := ReadToolkit(repo(t, map[string]string{"billing.lazyspec.md": "", "apps/web/SPEC.md": ""}))
		if err != nil || !slices.Equal(kit.Specs, []string{"lazyspec", "SPEC.md"}) {
			t.Fatalf("specs = %q, %v, want lazyspec and SPEC.md", kit.Specs, err)
		}
		hidden := map[string]string{}
		for _, d := range []string{"node_modules", ".git", "vendor", "dist", "build"} {
			hidden[d+"/pkg/SPEC.md"] = ""
			hidden[d+"/x.lazyspec.md"] = ""
		}
		kit, err = ReadToolkit(repo(t, hidden))
		if err != nil || len(kit.Specs) != 0 {
			t.Fatalf("specs = %q, %v, want none: those directories are not the repository's own", kit.Specs, err)
		}
	})

	t.Run("A Project's Checks Are Its Verify Commands, Else Its Toolkit's", func(t *testing.T) {
		npm := Toolkit{PackageManager: "npm", Lockfile: "package-lock.json", Checks: []string{"npm run lint", "npm run validate"}}
		got, err := Checks(&Project{Name: "eqms"}, npm)
		if err != nil || !slices.Equal(got, npm.Checks) {
			t.Fatalf("checks = %q, %v, want the toolkit's", got, err)
		}
		listed := &Project{Name: "eqms", Verify: []string{"npm run lint", "make check"}}
		if got, err := Checks(listed, npm); err != nil || !slices.Equal(got, listed.Verify) {
			t.Fatalf("checks = %q, %v, want the listed commands", got, err)
		}
		for _, kit := range []Toolkit{npm, {}} {
			_, err := Checks(&Project{Name: "eqms", Path: "/r", Verify: []string{"pnpm lint"}}, kit)
			want := kit.PackageManager
			if want == "" {
				want = "no package manager"
			}
			if err == nil || !strings.Contains(err.Error(), "pnpm lint") || !strings.Contains(err.Error(), want) {
				t.Fatalf("pnpm in a %q repo: %v, want it refused naming both", kit.PackageManager, err)
			}
		}
		unlocked := Toolkit{PackageManager: "npm", Checks: []string{"npm run test"}}
		bun := &Project{Name: "demo", Verify: []string{"bun test"}}
		if got, err := Checks(bun, unlocked); err != nil || !slices.Equal(got, bun.Verify) {
			t.Fatalf("checks = %q, %v, want bun run as listed where no lockfile names a manager", got, err)
		}
	})
}
