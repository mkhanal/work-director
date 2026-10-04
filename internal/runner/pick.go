package runner

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

// Role is what a model is being asked for. The two are not the same job, and
// spending the same budget on both would either overspend on one or underspend
// on the other.
type Role string

const (
	// RoleInterpret classifies text into a closed vocabulary and emits one
	// structured proposal. Label-shaped, runs on every message, and the floor is
	// low.
	RoleInterpret Role = "interpret"
	// RoleTaste judges whether a rule generalises or stays project-scoped.
	// Reasoning, not labelling, and it reaches every future run rather than one
	// task — so the floor here is the one that must not be lowered.
	RoleTaste Role = "taste"
)

// Roles is the closed set, in the order `wd model` lists them.
var Roles = []Role{RoleInterpret, RoleTaste}

// Cost is what a model costs per million tokens. Zero is what a free or
// locally-run model costs, and it is the reason a ladder can exist: cheapest and
// free are the same answer once the number is zero.
type Cost struct {
	Input  float64 `json:"input"`
	Output float64 `json:"output"`
}

// Total ranks two models. Blended rather than exact, because an exact blend
// would need a guess about the input:output ratio, and a guess here decides
// which model is trusted with the work.
func (c Cost) Total() float64 { return c.Input + c.Output }

// Model is one model a runner offers and what is known about it. Unknown things
// are zero or empty rather than guessed: a cost inferred from a model name is
// right until the free tier carrying that name is replaced, and then it is
// silently wrong in the direction that spends money.
type Model struct {
	ID     string `json:"id"`
	Cost   Cost   `json:"cost"`
	Status string `json:"status"`
	// CostKnown says the runner, or a person, actually stated a cost.
	CostKnown bool `json:"cost_known"`
}

// Verdict is one model's answer to "can you do this job".
type Verdict struct {
	// Capable is whether the model met the role's floor.
	Capable bool `json:"capable"`
	// Why is what it did or did not do, in words a person can act on. A model
	// that returned something the schema does not allow failed in a specific,
	// checkable way and the reason is worth keeping.
	Why string `json:"why"`
	// Probed is false when nothing was asked, because the answer was known or
	// the model was not reached.
	Probed bool `json:"probed"`
	// Unreachable means the runner could not be asked at all — not installed,
	// quota exhausted, no session. That is not the model failing; it is us not
	// knowing, and the two must not read the same.
	Unreachable bool `json:"unreachable"`
}

// Probe asks one model whether it can do a role's job, by making it attempt the
// real thing. This is a test rather than a declared capability because a
// declaration is a guess with extra steps, and because the thing we need is
// narrow and checkable: does it return what the schema allows, and does it
// decline when it should.
type Probe func(model Model, role Role) Verdict

// Policy is how a role picks. Every part is overridable, and the defaults are
// chosen to never name a model: a hardcoded id is right until the free tier that
// carried it is replaced.
type Policy struct {
	// Runners to try, in order. Empty means every detected runner in detection
	// order.
	Runners []string
	// Preferred are model names this role should take when one offers it and
	// one passes the floor. A stale entry costs nothing: it is only ever
	// selected from a runner that actually offers it.
	Preferred []string
	// Forced pins one model and is never second-guessed. It is an override, so
	// it is honoured even when it fails the floor — but Choice.Why records that
	// it did, because an override nobody warned about is not one.
	Forced string
	// Lister is how a runner's models are found. Nil asks the runner, which is
	// what production does; a test supplies its own so the ladder can be checked
	// without three fake CLIs on PATH. This is the only seam, deliberately: the
	// ordering rules are the part worth testing and they should be pure.
	Lister func(runner string) ([]Model, error)
}

// Choice is what a role will use, and how it got there.
type Choice struct {
	Runner string `json:"runner"`
	Model  string `json:"model"`
	// Why is the rung, in words. Not decoration: a person whose judgement was
	// made by an unexpected model needs to know whether it was configured, was
	// proven cheap and capable, or was merely all there was.
	Why string `json:"why"`
	// Considered is every candidate in the order they were tried, with the
	// verdict on each. `wd model for taste` prints this, because a ladder whose
	// steps are invisible is a ladder nobody can trust or debug.
	Considered []Step `json:"considered"`
	// Floor says whether the chosen model met the role's capability floor.
	Floor bool `json:"floor"`
}

// Step is one candidate and what happened when it was tried.
type Step struct {
	Model   string  `json:"model"`
	Cost    Cost    `json:"cost"`
	Verdict Verdict `json:"verdict"`
}

// Pick returns the model a role should use.
//
// The ladder is: forced, then a preferred model that passes the floor, then the
// cheapest model that passes the floor, then a runner's own default when nothing
// could be ranked, then a refusal. The third rung is the one the design exists
// for — candidates are walked cheapest-first and probed in that order, so the
// first that passes is by construction the cheapest that also does the job.
// Probing in cost order rather than probing everything is what keeps this cheap:
// it stops at the first pass instead of paying to rank 40 models.
//
// The fourth rung is honest rather than a fallback. No runner reports cost and
// claude reports no model list at all, so on a machine with no declarations the
// only answer wd can give is to ask the provider which model it wants — which is
// very likely the same thing a person would have got, and is never a guess wd
// invented.
func Pick(role Role, avail []Availability, costs map[string]Cost, probe Probe, p Policy) (Choice, error) {
	if p.Forced != "" {
		return Choice{Model: p.Forced, Why: "forced by configuration; the floor was not applied", Floor: false}, nil
	}

	order := p.Runners
	if len(order) == 0 {
		for _, a := range avail {
			if a.Detected {
				order = append(order, a.Runner)
			}
		}
	}
	if len(order) == 0 {
		return Choice{}, fmt.Errorf("no runner is detected: wd doctor says which provider CLIs are on PATH")
	}

	var everything []Model
	var unrankable string

	for _, name := range order {
		a := avail[indexOf(avail, name)]
		if !a.Detected {
			continue
		}
		list := p.Lister
		if list == nil {
			list = func(r string) ([]Model, error) { return runnerModels(r, costs) }
		}
		models, err := list(name)
		if err != nil || len(models) == 0 {
			if unrankable == "" {
				unrankable = name
			}
			continue
		}
		everything = append(everything, models...)
	}

	if len(everything) == 0 {
		if unrankable != "" {
			return Choice{Runner: unrankable, Model: "",
				Why: fmt.Sprintf("%s reports no models wd can rank, so it uses its own default rather than one wd invented", unrankable)}, nil
		}
		return Choice{}, fmt.Errorf("no runner offers a model %s could rank: every detected runner either reports nothing or is not on PATH", role)
	}

	for _, want := range p.Preferred {
		for _, m := range everything {
			if !matches(m.ID, want) {
				continue
			}
			v := probeOf(probe, m, role)
			if v.Capable {
				return Choice{Runner: runnerOf(m.ID), Model: m.ID, Floor: true,
					Why:        fmt.Sprintf("configured as preferred for %s, and it meets the floor", role),
					Considered: []Step{{Model: m.ID, Cost: m.Cost, Verdict: v}}}, nil
			}
			return Choice{}, fmt.Errorf("%s was configured as preferred for %s but does not meet the floor: %s — force it with --model if that is what you want", m.ID, role, v.Why)
		}
	}

	// Cheapest first, so the first that passes is the cheapest that can do the
	// job. Models with no declared cost are treated as unrankable rather than
	// as free: unknown is not zero, and guessing it as zero is how a judgement
	// starts costing money without anyone deciding that it should.
	ranked := slices.Clone(everything)
	slices.SortStableFunc(ranked, func(x, y Model) int {
		if x.CostKnown != y.CostKnown {
			if x.CostKnown {
				return -1
			}
			return 1
		}
		if !x.CostKnown {
			return 0
		}
		switch {
		case x.Cost.Total() < y.Cost.Total():
			return -1
		case x.Cost.Total() > y.Cost.Total():
			return 1
		}
		return 0
	})

	considered := []Step{}
	anyProbed, anyFailed, anyUnreachable := false, false, false
	for _, m := range ranked {
		v := probeOf(probe, m, role)
		considered = append(considered, Step{Model: m.ID, Cost: m.Cost, Verdict: v})
		// Only a model that was asked and answered can pass. Not probed is
		// unknown and unreachable is us failing to find out; neither is a pass,
		// and neither is a failure either — and the refusal has to say which it
		// was or the ladder cannot be debugged.
		if v.Capable {
			return Choice{Runner: runnerOf(m.ID), Model: m.ID, Floor: true,
				Why:        cheapestWhy(m, role, len(considered), len(ranked)),
				Considered: considered}, nil
		}
		switch {
		case v.Unreachable:
			anyUnreachable = true
		case v.Probed:
			anyProbed, anyFailed = true, true
		}
	}

	// The fallback to a runner's own default is for not knowing, and only for
	// not knowing. Once candidates have actually been probed and answered, they
	// failed, and quietly switching runners would report a floor nobody applied.
	// So: no floor data at all means ask; a floor that was checked and not met
	// means refuse and say what each model did.
	if !anyProbed && !anyUnreachable {
		if unrankable != "" {
			return Choice{Runner: unrankable,
				Why:        fmt.Sprintf("the %s floor was not checked on any candidate, so %s is asked instead of one being called incapable: see `wd model for %s`", role, unrankable, role),
				Considered: considered}, nil
		}
	} else if anyUnreachable && !anyFailed {
		return Choice{}, fmt.Errorf("no %s model could be asked at all: %s — the models are there and nothing was wrong with them", role, describeFailures(considered))
	} else if anyFailed {
		return Choice{}, fmt.Errorf("no model met the %s floor: %s — raise it with --model, or see what each candidate did with `wd model for %s`",
			role, describeFailures(considered), role)
	}
	if unrankable != "" {
		return Choice{Runner: unrankable,
			Why:        fmt.Sprintf("nothing ranked met the %s floor, so %s is asked instead: see `wd model for %s`", role, unrankable, role),
			Considered: considered}, nil
	}
	return Choice{}, fmt.Errorf("no model met the %s floor: %s — raise it with --model, or see what each candidate did with `wd model for %s`",
		role, describeFailures(considered), role)
}

// probeOf runs the probe, and treats a probe that could not be asked as a
// non-answer rather than a failure. A model behind an exhausted quota has not
// been shown to be incapable, and the two must not collapse into one.
func probeOf(probe Probe, m Model, role Role) Verdict {
	if probe == nil {
		return Verdict{Why: "no probe is configured, so the floor was not checked"}
	}
	return probe(m, role)
}

func cheapestWhy(m Model, role Role, tried, total int) string {
	base := fmt.Sprintf("the cheapest of %d candidates that met the %s floor", tried, role)
	if m.Cost.Total() == 0 && m.CostKnown {
		return base + ", and free"
	}
	return base
}

func describeFailures(steps []Step) string {
	if len(steps) == 0 {
		return "nothing was tried"
	}
	parts := make([]string, 0, len(steps))
	for _, s := range steps {
		why := s.Verdict.Why
		if why == "" {
			why = "did not meet the floor"
		}
		parts = append(parts, s.Model+" ("+why+")")
	}
	return strings.Join(parts, ", ")
}

func matches(id, want string) bool {
	return id == want || strings.HasSuffix(id, "/"+want)
}

func runnerOf(id string) string {
	if i := strings.Index(id, "/"); i > 0 {
		return id[:i]
	}
	return ""
}

func indexOf(as []Availability, name string) int {
	for i, a := range as {
		if a.Runner == name {
			return i
		}
	}
	return -1
}

// runnerModels is what a runner offers, with declared costs folded in.
func runnerModels(name string, costs map[string]Cost) ([]Model, error) {
	rn, err := DetectedRunner(name)
	if err != nil {
		return nil, err
	}
	ids, err := rn.Models()
	if err != nil {
		return nil, err
	}
	out := make([]Model, 0, len(ids))
	for _, id := range ids {
		if c, ok := costs[id]; ok {
			out = append(out, Model{ID: id, Cost: c, Status: "active", CostKnown: true})
			continue
		}
		out = append(out, Model{ID: id, Status: "active"})
	}
	return out, nil
}

// DeclaredCosts reads costs a person has stated, from $WD_HOME/models.json:
//
//	{ "opencode/some-model": { "input": 0, "output": 0 } }
//
// No runner's CLI reports what its models cost — opencode lists ids and no
// prices, claude lists nothing — so this file is where cost comes from. It is a
// declaration rather than a probe on purpose: a cost guessed from a model name
// would be right until the free tier carrying that name is replaced, and then
// wrong in the direction that spends money. An absent file is not an error; it
// means nothing is declared and the ladder falls back to the runner's choice.
func DeclaredCosts(home string) (map[string]Cost, error) {
	out := map[string]Cost{}
	path := filepath.Join(home, "models.json")
	raw, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return out, nil
}
