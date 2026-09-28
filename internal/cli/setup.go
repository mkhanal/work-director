package cli

import (
	"fmt"
	"strings"

	"wd/internal/install"
	"wd/internal/runner"
)

// setup probes the runtime dependencies and reports them; --install runs the
// install command for every missing one first. It exits 0 when every
// dependency is present and 1 when any is missing.
func (c *Cli) setup(rest []string) error {
	report := install.Probe()
	if flag(c.Args, "install") {
		report = install.Install(c.runInstall)
	}
	c.out(report, renderDeps(report))
	if missing := install.Missing(report); len(missing) > 0 {
		return fail("wd setup: missing %s", strings.Join(missing, ", "))
	}
	return nil
}

// renderDeps renders the dependency report as a table: name and status
// padded, then the path or the install command.
func renderDeps(deps []install.Dep) string {
	var b strings.Builder
	for _, dep := range deps {
		path := dep.Path
		if path == "" {
			path = dep.Install
		}
		fmt.Fprintf(&b, "%-8s %-8s %s\n", dep.Name, dep.Status, path)
	}
	return strings.TrimRight(b.String(), "\n")
}

// runInstall executes one install command, printing it and its failure; the
// re-probe after every command is the source of truth for what is installed.
func (c *Cli) runInstall(cmd string) {
	fmt.Fprintf(c.Stderr, "+ %s\n", cmd)
	r, err := runner.Run([]string{"sh", "-c", cmd}, "")
	if err != nil {
		fmt.Fprintf(c.Stderr, "install failed: %v\n", err)
		return
	}
	if r.Code != 0 {
		fmt.Fprintf(c.Stderr, "%s exited %d: %s\n", cmd, r.Code, strings.TrimSpace(r.Stderr))
	}
}
