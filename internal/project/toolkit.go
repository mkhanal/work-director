package project

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// Toolkit is what a repository itself says about how it is checked: the
// package manager its lockfile names, the checks its own manifests define, and
// the specifications it keeps. It is read from the repository, never assumed.
type Toolkit struct {
	// PackageManager is npm, pnpm, yarn or bun, or empty outside a node repo.
	PackageManager string
	// Lockfile is the lockfile PackageManager was read from, or empty when
	// none says which manager the repository uses.
	Lockfile string
	Checks         []string
	Specs          []string
}

// nodeScripts are the package.json scripts that check a repository, in the
// order they run.
var nodeScripts = []string{"lint", "typecheck", "check", "validate", "test"}

var packageManagers = []string{"npm", "pnpm", "yarn", "bun"}

// unsearched are directories that hold other people's code or build output.
var unsearched = []string{"node_modules", ".git", "vendor", "dist", "build"}

// ReadToolkit reads dir's toolkit.
func ReadToolkit(dir string) (Toolkit, error) {
	kit := Toolkit{Checks: []string{}, Specs: []string{}}
	manifest, err := os.ReadFile(filepath.Join(dir, "package.json"))
	switch {
	case err == nil:
		kit.PackageManager, kit.Lockfile = packageManager(dir)
		var pkg struct {
			Scripts map[string]string `json:"scripts"`
		}
		if err := json.Unmarshal(manifest, &pkg); err != nil {
			return Toolkit{}, fmt.Errorf("%s: %w", filepath.Join(dir, "package.json"), err)
		}
		for _, s := range nodeScripts {
			if _, ok := pkg.Scripts[s]; ok {
				kit.Checks = append(kit.Checks, kit.PackageManager+" run "+s)
			}
		}
	case !errors.Is(err, fs.ErrNotExist):
		return Toolkit{}, err
	}
	if exists(filepath.Join(dir, "go.mod")) {
		kit.Checks = append(kit.Checks, "go vet ./...", "go test ./...")
	}
	if exists(filepath.Join(dir, "Cargo.toml")) {
		kit.Checks = append(kit.Checks, "cargo test")
	}
	if kit.Specs, err = specs(dir); err != nil {
		return Toolkit{}, err
	}
	return kit, nil
}

func packageManager(dir string) (manager, lockfile string) {
	for _, lock := range []struct{ file, manager string }{
		{"pnpm-lock.yaml", "pnpm"},
		{"yarn.lock", "yarn"},
		{"bun.lock", "bun"},
		{"bun.lockb", "bun"},
		{"package-lock.json", "npm"},
	} {
		if exists(filepath.Join(dir, lock.file)) {
			return lock.manager, lock.file
		}
	}
	return "npm", ""
}

// specs names the specification conventions dir keeps.
func specs(dir string) ([]string, error) {
	var lazyspec, specMd bool
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != dir && slices.Contains(unsearched, d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		switch {
		case strings.HasSuffix(d.Name(), ".lazyspec.md"):
			lazyspec = true
		case d.Name() == "SPEC.md":
			specMd = true
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	out := []string{}
	if lazyspec {
		out = append(out, "lazyspec")
	}
	if specMd {
		out = append(out, "SPEC.md")
	}
	return out, nil
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// Checks are what verifies work in p: its listed verify commands, else what
// its repository's toolkit gives. A listed command for a package manager the
// repository does not use is refused rather than run to fail.
func Checks(p *Project, kit Toolkit) ([]string, error) {
	if len(p.Verify) == 0 {
		return kit.Checks, nil
	}
	for _, cmd := range p.Verify {
		tool, _, _ := strings.Cut(strings.TrimSpace(cmd), " ")
		if !slices.Contains(packageManagers, tool) || tool == kit.PackageManager {
			continue
		}
		uses := "no package manager: it has no package.json"
		if kit.PackageManager != "" {
			if kit.Lockfile == "" {
				continue
			}
			uses = kit.PackageManager + " (" + kit.Lockfile + ")"
		}
		defines := "nothing"
		if len(kit.Checks) > 0 {
			defines = strings.Join(kit.Checks, ", ")
		}
		return nil, fmt.Errorf("project %s verifies with `%s`, but %s uses %s; fix verify: in its project file, or remove it to run what the repository defines (%s)",
			p.Name, cmd, p.Path, uses, defines)
	}
	return p.Verify, nil
}
