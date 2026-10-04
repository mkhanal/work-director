package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"wd/internal/runner"
)

// model shows and sets which model each judgement role uses.
//
// Showing is the point as much as setting: `wd model for taste` prints every
// candidate and what happened when it was tried, because a ladder whose steps
// are invisible is a ladder nobody can trust when a promotion goes wrong.
func (c *Cli) model(rest []string) error {
	global, err := runner.LoadPolicy(c.Home)
	if err != nil {
		return err
	}
	sub := ""
	if len(rest) > 0 {
		sub = rest[0]
	}
	switch sub {
	case "for":
		if len(rest) < 2 {
			return fail("usage: wd model for <interpret|taste>")
		}
		return c.modelFor(global, rest[1])
	case "set":
		return c.modelSet(global, rest)
	case "reset":
		return c.modelReset(rest)
	case "":
		return c.modelFor(global, "")
	default:
		return fail("usage: wd model (for <role> | set <role> [--prefer m]… [--runners r]… [--model m] | reset <role>)")
	}
}

func parseRole(s string) (runner.Role, error) {
	for _, r := range runner.Roles {
		if string(r) == s {
			return r, nil
		}
	}
	return "", fail("usage: wd model for <%s>", joinRoles())
}

func joinRoles() string {
	names := make([]string, 0, len(runner.Roles))
	for _, r := range runner.Roles {
		names = append(names, string(r))
	}
	return strings.Join(names, "|")
}

// modelFor prints the choice for one role, or for both.
func (c *Cli) modelFor(global runner.PolicySet, role string) error {
	avail, _ := runner.Availabilities()
	roles := runner.Roles
	if role != "" {
		r, err := parseRole(role)
		if err != nil {
			return err
		}
		roles = []runner.Role{r}
	}
	type out struct {
		Role       string        `json:"role"`
		Runner     string        `json:"runner"`
		Model      string        `json:"model"`
		Why        string        `json:"why"`
		Floor      bool          `json:"floor"`
		Considered []runner.Step `json:"considered"`
	}
	list := []out{}
	table := []string{}
	for _, r := range roles {
		p := global.Policy(r)
		choice, err := runner.Pick(r, avail, nil, nil, runner.Policy{Role: p, Lister: c.listerFor})
		if err != nil {
			list = append(list, out{Role: string(r), Why: err.Error()})
			table = append(table, fmt.Sprintf("%s\trefused\t%s", r, err))
			continue
		}
		// A preference is checked when it is set, not when it is used: a name
		// no runner offers is refused here rather than silently ignored on
		// Tuesday while judgement keeps going somewhere else.
		for _, want := range p.Preferred {
			if err := c.preferenceOffered(want, avail); err != nil {
				return err
			}
		}
		list = append(list, out{Role: string(r), Runner: choice.Runner, Model: choice.Model,
			Why: choice.Why, Floor: choice.Floor, Considered: choice.Considered})
		table = append(table, fmt.Sprintf("%s\t%s\t%s\t%s", r, orDash(choice.Runner), orDash(choice.Model), choice.Why))
	}
	return c.out(list, strings.Join(table, "\n"))
}

// preferenceOffered refuses a preference no detected runner offers, naming the
// ones that exist. A preference that cannot be honoured is worth knowing about
// at the moment it is written rather than the first time it is ignored.
func (c *Cli) preferenceOffered(want string, avail []runner.Availability) error {
	for _, a := range avail {
		if !a.Detected {
			continue
		}
		models, err := c.listerFor(a.Runner)
		if err != nil {
			continue
		}
		for _, m := range models {
			if m.ID == want || strings.HasSuffix(m.ID, "/"+want) {
				return nil
			}
		}
	}
	var offered []string
	for _, a := range avail {
		if a.Detected {
			if models, err := c.listerFor(a.Runner); err == nil && len(models) > 0 {
				offered = append(offered, fmt.Sprintf("%s (%d models)", a.Runner, len(models)))
			} else {
				offered = append(offered, a.Runner+" (reports none)")
			}
		}
	}
	return fail("%s is not offered by any detected runner: %s — wd model for <role> lists them", want, strings.Join(offered, ", "))
}

// modelSet writes one role's policy. It refuses a preference no runner offers,
// and a forced model outside the runners the role allows, so a project or a
// person cannot write a policy that quietly does nothing or quietly widens one.
func (c *Cli) modelSet(global runner.PolicySet, rest []string) error {
	if len(rest) < 2 {
		return fail("usage: wd model set <interpret|taste> [--prefer m]… [--runners r]… [--model m]")
	}
	role, err := parseRole(rest[1])
	if err != nil {
		return err
	}
	p := global.Policy(role)
	if str(c.Args, "runners") != nil {
		p.Runners = csvFlags(c.Args, "runners")
	}
	if str(c.Args, "prefer") != nil {
		p.Preferred = csvFlags(c.Args, "prefer")
	}
	if v := strOr(c.Args, "model", ""); v != "" {
		p.Forced = v
	}
	avail, _ := runner.Availabilities()
	for _, want := range p.Preferred {
		if err := c.preferenceOffered(want, avail); err != nil {
			return err
		}
	}
	if p.Forced != "" && len(p.Runners) > 0 && !contains(p.Runners, runnerOfModel(p.Forced)) {
		return fail("--model %s is on runner %q, which --runners %s does not include",
			p.Forced, runnerOfModel(p.Forced), strings.Join(p.Runners, ","))
	}
	if role == runner.RoleTaste {
		global.Taste = p
	} else {
		global.Interpret = p
	}
	if err := writePolicy(c.Home, global); err != nil {
		return err
	}
	return c.out(p, fmt.Sprintf("%s policy set: runners=%s preferred=%s model=%s",
		role, orDash(strings.Join(p.Runners, ",")), orDash(strings.Join(p.Preferred, ",")), orDash(p.Forced)))
}

func (c *Cli) modelReset(rest []string) error {
	if len(rest) < 2 {
		return fail("usage: wd model reset <interpret|taste>")
	}
	role, err := parseRole(rest[1])
	if err != nil {
		return err
	}
	global, err := runner.LoadPolicy(c.Home)
	if err != nil {
		return err
	}
	if role == runner.RoleTaste {
		global.Taste = runner.RolePolicy{}
	} else {
		global.Interpret = runner.RolePolicy{}
	}
	if err := writePolicy(c.Home, global); err != nil {
		return err
	}
	return c.out(global, fmt.Sprintf("%s policy reset: every runner, no preference, no override", role))
}

// writePolicy keeps judge.json readable rather than minified: it is a file a
// person is meant to edit and argue with.
func writePolicy(home string, p runner.PolicySet) error {
	raw, err := runner.MarshalPolicy(p)
	if err != nil {
		return err
	}
	path := filepath.Join(home, "judge.json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		return err
	}
	return nil
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func runnerOfModel(id string) string {
	if i := strings.Index(id, "/"); i > 0 {
		return id[:i]
	}
	return ""
}
