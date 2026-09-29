package cli

import (
	"fmt"
	"strings"

	"wd/internal/context"
	"wd/internal/runner"
)

// Doctor is what the host offers the director: which registered runners are
// detected, and the repo state of the directory wd runs in. Nothing in it is
// a failure; an absent runner or a missing repo is information.
type Doctor struct {
	Runners   []runner.Availability `json:"runners"`
	Workspace context.Workspace     `json:"workspace"`
	Init      *string               `json:"init"`
}

// initHint is offered outside a repo and never run: the user decides whether
// the directory becomes one.
const initHint = "git init"

func (c *Cli) doctor(rest []string) error {
	runners, err := runner.Availabilities()
	if err != nil {
		return err
	}
	ws, err := context.WorkspaceContext(mustGetwd())
	if err != nil {
		return err
	}
	report := Doctor{Runners: runners, Workspace: ws}
	if ws.Repo == nil {
		hint := initHint
		report.Init = &hint
	}
	c.out(report, renderDoctor(report))
	return nil
}

// renderDoctor prints one line per runner, then the workspace line and, with
// no repo, the init option.
func renderDoctor(d Doctor) string {
	var b strings.Builder
	for _, a := range d.Runners {
		if a.Detected {
			fmt.Fprintf(&b, "%-10s detected      %s\n", a.Runner, *a.Path)
			continue
		}
		fmt.Fprintf(&b, "%-10s not detected  (%s not on PATH)\n", a.Runner, a.Command)
	}
	b.WriteString(context.ContextLine(d.Workspace))
	if d.Init != nil {
		fmt.Fprintf(&b, "\nnot a repo: run `%s` here to track work in one (optional)", *d.Init)
	}
	return b.String()
}
