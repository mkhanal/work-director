// Command taste builds the taste plugin: rule cards under taste/cards become
// plugin/skills/taste-*/SKILL.md, plugin/constitution.md and the dist
// artifacts. It is the Go rewrite of `bun run build`; the files it writes
// are generated and never hand-edited.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"wd/internal/taste"
)

const usage = "usage: taste build"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "taste: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) > 0 && args[0] != "build" {
		fmt.Fprintln(os.Stderr, usage)
		os.Exit(2)
	}
	root, err := repoRoot()
	if err != nil {
		return err
	}
	out, err := taste.Build(taste.BuildPaths{
		CardsDir:   filepath.Join(root, "taste", "cards"),
		PresetsDir: filepath.Join(root, "presets"),
		PluginDir:  filepath.Join(root, "plugin"),
		DistDir:    filepath.Join(root, "dist"),
	})
	if err != nil {
		return err
	}
	fmt.Printf("%d cards → %d skills, plugin/constitution.md, dist/AGENTS.fragment.md\n",
		out.Cards, len(out.Skills))
	return nil
}

// repoRoot locates the module root from this source file, so the build works
// from any working directory.
func repoRoot() (string, error) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "", fmt.Errorf("cannot locate taste source file")
	}
	return filepath.Dir(filepath.Dir(filepath.Dir(file))), nil
}
