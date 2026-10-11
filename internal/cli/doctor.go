package cli

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
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
	Config    Config                `json:"config"`
}

// Config is where each of the director's settings comes from. There is no one
// config file: a project is its own file, with its roadmap, and a runner is
// its own file of commands; this says where they all are.
type Config struct {
	Home string `json:"home"`
	// HomeFrom is WD_HOME when the environment set the home, else default.
	HomeFrom     string `json:"home_from"`
	Ledger       string `json:"ledger"`
	Projects     string `json:"projects"`
	ProjectCount int    `json:"project_count"`
	Runners      string `json:"runners"`
	// Cards is the checkout's cards directory, or "built-in".
	Cards string `json:"cards"`
	// OwnCards is the person's own cards directory, when there is one.
	OwnCards *string `json:"own_cards"`
}

// initHint is offered outside a repo and never run: the user decides whether
// the directory becomes one.
const initHint = "git init"

func (c *Cli) doctor(rest []string) error {
	runners, err := runner.Availabilities()
	if err != nil {
		return err
	}
	wd, err := os.Getwd()
	if err != nil {
		return err
	}
	ws, err := context.WorkspaceContext(wd)
	if err != nil {
		return err
	}
	cfg, err := c.config()
	if err != nil {
		return err
	}
	report := Doctor{Runners: runners, Workspace: ws, Config: cfg}
	if ws.Repo == nil {
		hint := initHint
		report.Init = &hint
	}
	return c.out(report, renderDoctor(report))
}

func (c *Cli) config() (Config, error) {
	cfg := Config{
		Home:         c.Home,
		HomeFrom:     "default",
		Ledger:       filepath.Join(c.Home, "ledger.db"),
		Projects:     c.ProjectsDir,
		ProjectCount: len(c.Projects),
		Runners:      filepath.Join(c.Home, "runners"),
		Cards:        "built-in",
	}
	if os.Getenv("WD_HOME") != "" {
		cfg.HomeFrom = "WD_HOME"
	}
	checkout, err := tasteCheckout()
	if err != nil {
		return Config{}, err
	}
	if checkout != "" {
		cfg.Cards = checkout
	}
	own := filepath.Join(c.Home, "taste", "cards")
	if st, err := os.Stat(own); err == nil && st.IsDir() {
		cfg.OwnCards = &own
	} else if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return Config{}, err
	}
	return cfg, nil
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
	cfg := d.Config
	fmt.Fprintf(&b, "\nhome: %s (%s)\nledger: %s\nprojects: %s (%d)\nrunner files: %s\ntaste cards: %s",
		cfg.Home, cfg.HomeFrom, cfg.Ledger, cfg.Projects, cfg.ProjectCount, cfg.Runners, cfg.Cards)
	if cfg.OwnCards != nil {
		fmt.Fprintf(&b, ", and yours in %s", *cfg.OwnCards)
	}
	if d.Init != nil {
		fmt.Fprintf(&b, "\nnot a repo: run `%s` here to track work in one (optional)", *d.Init)
	}
	return b.String()
}
