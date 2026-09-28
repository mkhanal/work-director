package taste

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const ConstitutionLimit = 2000
const FragmentBegin = "<!-- taste:begin -->"
const FragmentEnd = "<!-- taste:end -->"

var SkillDescriptions = map[Category]string{
	CategoryJudgment:        "Use before any analysis or design decision: how to size a need, a gap, a cost, a fix.",
	CategoryAlternatives:    "Use when weighing options or when a settled decision resurfaces.",
	CategoryOrganisation:    "Use when naming, placing or structuring code, modules, tests or data access.",
	CategoryTypesAndSchemas: "Use when writing TypeScript types, parsing input, or touching a schema.",
	CategoryDefensiveCoding: "Use when tempted to add a check, wrapper, flag, catch, suppression or branch.",
	CategoryComments:        "Use when about to write a comment.",
	CategoryWorkingMethod:   "Use when planning a task, delegating, testing, or about to claim something is done.",
	CategoryCommunication:   "Use when writing a reply, report or brief.",
}

// Presets are the lint rule ids a card's enforce entries may reference.
type Presets struct {
	Biome  map[string]bool
	Eslint map[string]bool
}

type biomeFile struct {
	Linter struct {
		Rules map[string]json.RawMessage `json:"rules"`
	} `json:"linter"`
}

// LoadCards reads every card under cardsDir, one subdirectory per category,
// and returns them sorted by id.
func LoadCards(cardsDir string) ([]Card, error) {
	var cards []Card
	dirs, err := os.ReadDir(cardsDir)
	if err != nil {
		return nil, err
	}
	for _, d := range dirs {
		if !d.IsDir() {
			continue
		}
		files, err := os.ReadDir(filepath.Join(cardsDir, d.Name()))
		if err != nil {
			return nil, err
		}
		for _, f := range files {
			if !strings.HasSuffix(f.Name(), ".md") {
				continue
			}
			path := filepath.Join(cardsDir, d.Name(), f.Name())
			text, err := os.ReadFile(path)
			if err != nil {
				return nil, err
			}
			card, err := ParseCard(string(text), path)
			if err != nil {
				return nil, err
			}
			cards = append(cards, *card)
		}
	}
	sort.Slice(cards, func(i, j int) bool { return cards[i].ID < cards[j].ID })
	return cards, nil
}

// LoadPresets reads the biome and eslint rule ids from the presets
// directory. Biome rules are grouped: a card references group/name.
func LoadPresets(presetsDir string) (*Presets, error) {
	biomeText, err := os.ReadFile(filepath.Join(presetsDir, "biome", "biome.json"))
	if err != nil {
		return nil, err
	}
	var biome biomeFile
	if err := json.Unmarshal(biomeText, &biome); err != nil {
		return nil, err
	}
	p := &Presets{Biome: map[string]bool{}, Eslint: map[string]bool{}}
	for group, raw := range biome.Linter.Rules {
		var rules map[string]json.RawMessage
		if err := json.Unmarshal(raw, &rules); err != nil {
			continue
		}
		for name := range rules {
			p.Biome[group+"/"+name] = true
		}
	}
	eslintText, err := os.ReadFile(filepath.Join(presetsDir, "eslint", "rules.json"))
	if err != nil {
		return nil, err
	}
	var eslint map[string]json.RawMessage
	if err := json.Unmarshal(eslintText, &eslint); err != nil {
		return nil, err
	}
	for name := range eslint {
		p.Eslint[name] = true
	}
	return p, nil
}

// Distributable keeps the cards that reach the artifacts: adopted, with no
// project scope.
func Distributable(cards []Card) []Card {
	var out []Card
	for _, c := range cards {
		if c.Status != StatusAdopted {
			continue
		}
		project := false
		for _, s := range c.Scope {
			if strings.HasPrefix(string(s), "project:") {
				project = true
				break
			}
		}
		if !project {
			out = append(out, c)
		}
	}
	return out
}

// MissingEnforcements names every enforce entry whose rule is absent from
// the presets, as card id plus the entry.
func MissingEnforcements(cards []Card, presets *Presets) []string {
	var missing []string
	for _, c := range cards {
		for _, e := range c.Enforce {
			tool, rule, _ := strings.Cut(e, ":")
			var ok bool
			switch tool {
			case "biome":
				ok = presets.Biome[rule]
			case "eslint":
				ok = presets.Eslint[rule]
			}
			if !ok {
				missing = append(missing, fmt.Sprintf("%s: %s", c.ID, e))
			}
		}
	}
	return missing
}

// RenderConstitution renders the always cards as title plus statement. It
// fails when the result exceeds the limit.
func RenderConstitution(cards []Card) (string, error) {
	var lines []string
	for _, c := range cards {
		if c.Always {
			lines = append(lines, fmt.Sprintf("- **%s.** %s", c.Title, c.Statement))
		}
	}
	text := "# Taste\nFull cards: /taste-* skills.\n" + strings.Join(lines, "\n") + "\n"
	if len(text) > ConstitutionLimit {
		return "", fmt.Errorf("constitution is %d chars, limit %d", len(text), ConstitutionLimit)
	}
	return text, nil
}

// RenderSkill renders one category's skill file: a section per card.
func RenderSkill(category Category, cards []Card) string {
	var sections []string
	for _, c := range cards {
		if c.Category == category {
			sections = append(sections, fmt.Sprintf("## %s\n%s\n", c.Title, c.Body))
		}
	}
	return fmt.Sprintf("---\nname: taste-%s\ndescription: %s\n---\n# Taste: %s\n\n%s",
		category, SkillDescriptions[category], category, strings.Join(sections, "\n"))
}

// RenderFragment wraps the constitution in taste markers for injection into
// an agent's instructions.
func RenderFragment(constitution string) string {
	return FragmentBegin + "\n" + constitution + FragmentEnd + "\n"
}

type BuildPaths struct {
	CardsDir   string
	PresetsDir string
	PluginDir  string
	DistDir    string
}

type BuildResult struct {
	Cards  int
	Skills []string
}

// Build renders the distributable cards into the plugin skills, the
// constitution and the dist artifacts. It fails when an enforce id is absent
// from the presets or the constitution exceeds its limit.
func Build(p BuildPaths) (*BuildResult, error) {
	loaded, err := LoadCards(p.CardsDir)
	if err != nil {
		return nil, err
	}
	cards := Distributable(loaded)
	presets, err := LoadPresets(p.PresetsDir)
	if err != nil {
		return nil, err
	}
	if missing := MissingEnforcements(cards, presets); len(missing) > 0 {
		return nil, fmt.Errorf("enforce ids absent from presets:\n%s", strings.Join(missing, "\n"))
	}
	constitution, err := RenderConstitution(cards)
	if err != nil {
		return nil, err
	}
	skillsDir := filepath.Join(p.PluginDir, "skills")
	if err := os.RemoveAll(skillsDir); err != nil {
		return nil, err
	}
	var skills []string
	for _, category := range Categories {
		inCategory := make([]Card, 0, len(cards))
		for _, c := range cards {
			if c.Category == category {
				inCategory = append(inCategory, c)
			}
		}
		if len(inCategory) == 0 {
			continue
		}
		dir := filepath.Join(skillsDir, "taste-"+string(category))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, err
		}
		text := RenderSkill(category, inCategory)
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(text), 0o644); err != nil {
			return nil, err
		}
		skills = append(skills, "taste-"+string(category))
	}
	if err := os.MkdirAll(p.DistDir, 0o755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(p.PluginDir, "constitution.md"), []byte(constitution), 0o644); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(p.DistDir, "constitution.md"), []byte(constitution), 0o644); err != nil {
		return nil, err
	}
	fragment := RenderFragment(constitution)
	if err := os.WriteFile(filepath.Join(p.DistDir, "AGENTS.fragment.md"), []byte(fragment), 0o644); err != nil {
		return nil, err
	}
	return &BuildResult{Cards: len(cards), Skills: skills}, nil
}
