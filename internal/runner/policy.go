package runner

import (
	"fmt"
	"strings"
)

// RolePolicy is what one role does for one project: which runners are allowed,
// which models to try first, and which single model overrides all of it.
//
// A project inherits the global policy and may narrow it. It may never widen
// it. That asymmetry is the whole of the safety property: a project that is
// forbidden from reaching a provider cannot re-allow it in its own file, or
// closing a provider globally would be one project file away from undone.
type RolePolicy struct {
	// Runners are the only providers this role may use, in order. Empty means
	// every detected runner, which is what an unconfigured director does.
	Runners []string `json:"runners"`
	// Preferred are models tried first, and only if they meet the floor.
	// Empty means no preference.
	Preferred []string `json:"preferred"`
	// Forced pins one model and skips the floor. Empty means not forced.
	Forced string `json:"model"`
	// PromoteGlobal says whether a card from this project may be promoted to
	// global taste. Unset means true, because global taste is the director's own
	// engineering taste and travels to every project by design while project
	// rules live in the project and never travel. It is a pointer so that unset
	// is distinguishable from an explicit no — a plain bool would make every
	// unconfigured project inherit "no", which is the opposite of the default.
	PromoteGlobal *bool `json:"promote_global,omitempty"`
}

// Policy is what each role does on this machine. A role with no entry is the
// zero value, which means every runner and no preference.
type PolicySet struct {
	Interpret RolePolicy `json:"interpret"`
	Taste     RolePolicy `json:"taste"`
}

// Policy returns the policy for one role, by value so a caller cannot edit the
// set it was handed.
func (p PolicySet) Policy(role Role) RolePolicy {
	if role == RoleTaste {
		return p.Taste
	}
	return p.Interpret
}

// Resolve narrows a global policy with a project's own, and refuses anything a
// project asks for that the global policy does not allow.
//
// A project may forbid and may reorder. It may not permit. So a runner list on
// the project is checked against the global list rather than replacing it, and a
// forced model is refused outright when the global runners would not have
// allowed it — naming what is allowed, because "not permitted" without the
// alternative is a dead end.
func Resolve(global, project RolePolicy, role Role, allowed []string) (RolePolicy, error) {
	out := global

	if len(project.Runners) > 0 {
		for _, r := range project.Runners {
			if !slicesContains(global.Runners, r) {
				return RolePolicy{}, fmt.Errorf("project %s policy may not allow runner %q for %s: %s — a project can narrow the policy, never widen it",
					role, r, role, permittedWording(global.Runners, allowed))
			}
		}
		out.Runners = project.Runners
	}
	if len(project.Preferred) > 0 {
		out.Preferred = project.Preferred
	}
	if project.Forced != "" {
		if len(out.Runners) > 0 && !slicesContains(out.Runners, runnerOf(project.Forced)) {
			return RolePolicy{}, fmt.Errorf("project policy forces %s for %s, whose runner %q is not among the runners allowed here (%s): %s",
				project.Forced, role, runnerOf(project.Forced), strings.Join(out.Runners, ", "), permittedWording(global.Runners, allowed))
		}
		out.Forced = project.Forced
	}
	// Same asymmetry as the runners: a project may hold its cards still and may
	// not release them, so an explicit yes against a global no is refused rather
	// than silently winning.
	switch {
	case project.PromoteGlobal == nil:
		// inherit whatever the machine said, which itself defaults to true
	case *project.PromoteGlobal && !globalAllows(global.PromoteGlobal):
		return RolePolicy{}, fmt.Errorf("project policy may not allow global promotion for %s: the machine has it off — a project can narrow the policy, never widen it", role)
	default:
		out.PromoteGlobal = project.PromoteGlobal
	}
	if out.PromoteGlobal == nil {
		on := true
		out.PromoteGlobal = &on
	}
	return out, nil
}

// permittedWording says what is allowed in terms a person can act on: the
// declared list if there is one, otherwise the runners actually present, since
// "every runner" is not an answer when the answer is "claude, because that is
// all you have".
// globalAllows reads a pointer that may be unset, and unset means the default:
// promotion is on unless something said otherwise.
func globalAllows(v *bool) bool {
	return v == nil || *v
}

func permittedWording(globalRunners, present []string) string {
	if len(globalRunners) > 0 {
		return "the global policy allows " + strings.Join(globalRunners, ", ")
	}
	if len(present) > 0 {
		return "detected: " + strings.Join(present, ", ")
	}
	return "no runner is detected"
}

func slicesContains(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}

// Allowed reports whether a model may be used under a policy, which is what
// makes `/model` in a conversation checkable rather than decorative: the switch
// changes the preference inside the allowlist and cannot leave it.
func (p RolePolicy) Allowed(model string) error {
	if p.Forced != "" {
		if model != p.Forced {
			return fmt.Errorf("%s is not in use: %s is forced for this role — change it with wd model set --model %s", model, p.Forced, p.Forced)
		}
		return nil
	}
	if len(p.Runners) == 0 {
		return nil
	}
	if !slicesContains(p.Runners, runnerOf(model)) {
		return fmt.Errorf("%s is not allowed here: this policy allows %s — change it with wd model set --runners %s",
			model, strings.Join(p.Runners, ", "), strings.Join(p.Runners, ","))
	}
	return nil
}

// RejectRunners says which runner is not permitted and what is, in the words a
// person can act on. A refusal that only says no is a dead end.
func RejectRunners(allowed, present []string) string {
	if len(allowed) > 0 {
		return "this project allows " + strings.Join(allowed, ", ")
	}
	return "no runner is detected"
}

// PromotesGlobal is whether promotion is on, with unset meaning the default.
func PromotesGlobal(p RolePolicy) bool { return globalAllows(p.PromoteGlobal) }

// BoolPtr is for callers building a policy from flags, where nil means unset.
func BoolPtr(v bool) *bool { return &v }
