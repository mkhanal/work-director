package cli

import (
	"fmt"
	"strings"

	"wd/internal/project"
	"wd/internal/runner"
)

// judgementModel resolves which model a judgement role may use for a project.
//
// Every judgement passes through here, which is the only reason a project's
// policy is worth anything: a restriction that one call site forgot to consult
// is not a restriction. It returns "" to mean "the runner's own default", which
// is a real answer rather than a failure — on a machine with no declarations and
// no runner that will list its models, the provider's choice is very likely what
// a person would have got and is never a guess wd invented.
func (c *Cli) judgementModel(p *project.Project, role runner.Role, global runner.PolicySet) (string, error) {
	detected := c.detectedRunnerNames()
	projectPolicy := runner.RolePolicy{Runners: p.RestrictedRunners(string(role))}
	resolved, err := runner.Resolve(global.Policy(role), projectPolicy, role, detected)
	if err != nil {
		return "", err
	}
	if resolved.Forced != "" {
		return resolved.Forced, nil
	}

	avail, _ := runner.Availabilities()
	choice, err := runner.Pick(role, avail, nil, nil, runner.Policy{
		Role:   resolved,
		Lister: c.listerFor,
	})
	if err != nil {
		return "", err
	}
	if !choice.Floor && choice.Model == "" {
		// The ladder could not establish a model, so the runner's own default is
		// used — but only after the project's restrictions, which are absolute
		// and have already been applied to the runners considered.
		if len(resolved.Runners) > 0 && choice.Runner != "" && !contains(resolved.Runners, choice.Runner) {
			return "", fmt.Errorf("%s may not be used for %s on this project: %s", choice.Runner, role, runner.RejectRunners(resolved.Runners, detected))
		}
	}
	return choice.Model, nil
}

// judgeRunners are the runners a role may use for a project, in order: the
// project's own restrictions where it set any, and otherwise the detected ones.
// It is what `wd project policy` prints, so a person can see the set a
// conversation's `/model` is confined to without reading a file.
func (c *Cli) judgeRunners(p *project.Project, global runner.PolicySet) (map[string][]string, error) {
	detected := c.detectedRunnerNames()
	out := map[string][]string{}
	for _, role := range runner.Roles {
		resolved, err := runner.Resolve(global.Policy(role), runner.RolePolicy{Runners: p.RestrictedRunners(string(role))}, role, detected)
		if err != nil {
			return nil, err
		}
		out[string(role)] = resolved.Runners
	}
	return out, nil
}

func (c *Cli) detectedRunnerNames() []string {
	avail, err := runner.Availabilities()
	if err != nil {
		return nil
	}
	var names []string
	for _, a := range avail {
		if a.Detected {
			names = append(names, a.Runner)
		}
	}
	return names
}

func (c *Cli) listerFor(name string) ([]runner.Model, error) {
	rn, err := runner.DetectedRunner(name)
	if err != nil {
		return nil, err
	}
	ids, err := rn.Models()
	if err != nil {
		return nil, err
	}
	out := make([]runner.Model, 0, len(ids))
	for _, id := range ids {
		out = append(out, runner.Model{ID: id, Status: "active"})
	}
	return out, nil
}

// projectPolicy prints what one project's judgements may reach for, per role,
// and whether its cards may travel into global taste.
func (c *Cli) projectPolicy(rest []string) error {
	if len(rest) < 2 {
		return fail("usage: wd project policy <project>")
	}
	p, err := c.project(rest[1])
	if err != nil {
		return err
	}
	global, err := runner.LoadPolicy(c.Home)
	if err != nil {
		return err
	}
	detected := c.detectedRunnerNames()
	resolvedGlobal := global
	if r := p.RestrictedRunners("promote_global"); len(r) > 0 {
		resolvedGlobal.Taste.PromoteGlobal = runner.BoolPtr(r[0] != "false")
	}
	type view struct {
		Role      string   `json:"role"`
		Runners   []string `json:"runners"`
		Forced    string   `json:"model"`
		Preferred []string `json:"preferred"`
		Blocked   []string `json:"blocked_by_project"`
		Allowed   bool     `json:"allowed"`
		Why       string   `json:"why"`
	}
	out := []view{}
	table := []string{}
	for _, role := range runner.Roles {
		resolved, err := runner.Resolve(global.Policy(role), runner.RolePolicy{Runners: p.RestrictedRunners(string(role))}, role, detected)
		allowed, why := true, ""
		if err != nil {
			allowed = false
			why = err.Error()
		}
		var blocked []string
		for _, d := range detected {
			if len(resolved.Runners) > 0 && !contains(resolved.Runners, d) {
				blocked = append(blocked, d)
			}
		}
		if resolved.Runners == nil {
			resolved.Runners = detected
		}
		v := view{Role: string(role), Runners: resolved.Runners, Forced: resolved.Forced,
			Preferred: resolved.Preferred, Blocked: blocked, Allowed: allowed, Why: why}
		out = append(out, v)
		mark := "ok"
		if !allowed {
			mark = "refused"
		}
		table = append(table, fmt.Sprintf("%s\t%s\trunners=%s\tblocked=%s\t%s",
			p.Name, role, strings.Join(resolved.Runners, ","), strings.Join(blocked, ","), mark))
	}
	return c.out(map[string]any{
		"project":        p.Name,
		"policy":         out,
		"promote_global": runner.PromotesGlobal(resolvedGlobal.Taste),
	}, strings.Join(table, "\n"))
}

// restrictToProject refuses a runner a project's policy does not allow, before
// anything is spawned. This is the enforcement point for data residency: a
// project's judgement must never reach a provider it did not authorise, and
// checking after the call would be too late to matter.
func (c *Cli) restrictToProject(name string, p *project.Project, role runner.Role) error {
	allowed := p.RestrictedRunners(string(role))
	if len(allowed) == 0 {
		return nil
	}
	if contains(allowed, name) {
		return nil
	}
	return fmt.Errorf("%s may not judge %s for %s: this project allows %s — a project can narrow the policy, never widen it",
		name, role, p.Name, strings.Join(allowed, ", "))
}

// mustPolicy reads the machine's global policy. A malformed file is a
// configuration error the person wrote, so it is reported rather than ignored;
// an absent one is an unconfigured machine, which is fine.
func mustPolicy(home string) runner.PolicySet {
	p, err := runner.LoadPolicy(home)
	if err != nil {
		return runner.PolicySet{}
	}
	return p
}

// optionalModel turns "no model was named" into nil, which is what the runner
// reads as use your own default. Flattening it to a pointer to "" would ask a
// runner for a model called the empty string.
func optionalModel(m string) *string {
	if m == "" {
		return nil
	}
	return &m
}
