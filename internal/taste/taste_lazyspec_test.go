package taste

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func card(fields map[string]string, body string) string {
	get := func(k, def string) string {
		if v, ok := fields[k]; ok {
			return v
		}
		return def
	}
	return "---\n" +
		"id: " + get("id", "x") + "\n" +
		"title: " + get("title", "Title X") + "\n" +
		"category: " + get("category", "judgment") + "\n" +
		"scope: " + get("scope", "[global]") + "\n" +
		"kind: " + get("kind", "principle") + "\n" +
		"status: " + get("status", "adopted") + "\n" +
		"always: " + get("always", "false") + "\n" +
		"enforce: " + get("enforce", "[]") + "\n" +
		"evidence: []\n" +
		"---\n" + body + "\n"
}

const cardBody = "Statement.\n**Why:** w\n**Apply:** a"

type fixture struct {
	root  string
	paths BuildPaths
}

// fixture writes cards and presets into a temp directory and returns the
// build paths over it.
func newFixture(t *testing.T, cards map[string]string, biomeRules map[string]any) *fixture {
	t.Helper()
	root := t.TempDir()
	for rel, text := range cards {
		path := filepath.Join(root, "cards", rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir cards: %v", err)
		}
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatalf("write card: %v", err)
		}
	}
	writeJSON(t, filepath.Join(root, "presets", "biome", "biome.json"),
		map[string]any{"linter": map[string]any{"rules": biomeRules}})
	writeJSON(t, filepath.Join(root, "presets", "eslint", "rules.json"),
		map[string]any{"no-else-return": "error"})
	return &fixture{
		root: root,
		paths: BuildPaths{
			CardsDir:   filepath.Join(root, "cards"),
			PresetsDir: filepath.Join(root, "presets"),
			PluginDir:  filepath.Join(root, "plugin"),
			DistDir:    filepath.Join(root, "dist"),
		},
	}
}

func writeJSON(t *testing.T, path string, v any) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir presets: %v", err)
	}
	text, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal presets: %v", err)
	}
	if err := os.WriteFile(path, text, 0o644); err != nil {
		t.Fatalf("write presets: %v", err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	text, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(text)
}

func wantErr(t *testing.T, err error, msg string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error %q, got nil", msg)
	}
	if err.Error() != msg {
		t.Fatalf("expected error %q, got %q", msg, err.Error())
	}
}

func TestCardMissingARequiredFieldIsRejected(t *testing.T) {
	text := strings.Replace(card(map[string]string{}, cardBody), "kind: principle\n", "", 1)
	_, err := ParseCard(text, "c/x.md")
	wantErr(t, err, "c/x.md: missing field kind")
}

func TestOnlyAdoptedNonProjectCardsReachTheArtifacts(t *testing.T) {
	f := newFixture(t, map[string]string{
		"judgment/a.md": card(map[string]string{"id": "a", "title": "Adopted One", "always": "true"}, cardBody),
		"judgment/b.md": card(map[string]string{"id": "b", "title": "Candidate One", "status": "candidate", "always": "true"}, cardBody),
		"judgment/c.md": card(map[string]string{"id": "c", "title": "Retired One", "status": "retired"}, cardBody),
		"judgment/d.md": card(map[string]string{"id": "d", "title": "Project One", "scope": "[project:foo]", "always": "true"}, cardBody),
	}, map[string]any{"style": map[string]any{"noEnum": "error"}})
	if _, err := Build(f.paths); err != nil {
		t.Fatalf("build: %v", err)
	}
	skill := readFile(t, filepath.Join(f.paths.PluginDir, "skills", "taste-judgment", "SKILL.md"))
	constitution := readFile(t, filepath.Join(f.paths.PluginDir, "constitution.md"))
	if !strings.Contains(skill, "Adopted One") {
		t.Error("skill missing Adopted One")
	}
	for _, gone := range []string{"Candidate One", "Retired One", "Project One"} {
		if strings.Contains(skill, gone) {
			t.Errorf("skill contains %q", gone)
		}
		if strings.Contains(constitution, gone) {
			t.Errorf("constitution contains %q", gone)
		}
	}
}

func TestEveryCategoryWithCardsBecomesOneSkill(t *testing.T) {
	f := newFixture(t, map[string]string{
		"judgment/a.md": card(map[string]string{"id": "a", "title": "Judge A"}, cardBody),
		"comments/b.md": card(map[string]string{"id": "b", "title": "Comment B", "category": "comments"}, cardBody),
	}, map[string]any{"style": map[string]any{"noEnum": "error"}})
	out, err := Build(f.paths)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if len(out.Skills) != 2 || out.Skills[0] != "taste-judgment" || out.Skills[1] != "taste-comments" {
		t.Errorf("skills = %v", out.Skills)
	}
	entries, err := os.ReadDir(filepath.Join(f.paths.PluginDir, "skills"))
	if err != nil {
		t.Fatalf("read skills: %v", err)
	}
	if len(entries) != 2 {
		t.Errorf("skills dir has %d entries", len(entries))
	}
	comments := readFile(t, filepath.Join(f.paths.PluginDir, "skills", "taste-comments", "SKILL.md"))
	if !strings.Contains(comments, "## Comment B") {
		t.Error("taste-comments skill missing ## Comment B")
	}
}

func TestConstitutionHoldsOnlyAlwaysCardsAndStaysUnderTheLimit(t *testing.T) {
	always, err := ParseCard(card(map[string]string{"id": "a", "title": "Always A", "always": "true"}, "Do A.\n**Why:** w"), "a")
	if err != nil {
		t.Fatalf("parse always: %v", err)
	}
	never, err := ParseCard(card(map[string]string{"id": "b", "title": "Never B"}, "b"), "b")
	if err != nil {
		t.Fatalf("parse never: %v", err)
	}
	text, err := RenderConstitution([]Card{*always, *never})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if !strings.Contains(text, "- **Always A.** Do A.") {
		t.Error("constitution missing always card")
	}
	if strings.Contains(text, "Never B") {
		t.Error("constitution contains non-always card")
	}
	big, err := ParseCard(card(map[string]string{"id": "big", "title": "Big", "always": "true"}, strings.Repeat("x", ConstitutionLimit)), "big")
	if err != nil {
		t.Fatalf("parse big: %v", err)
	}
	prefix := "# Taste\nFull cards: /taste-* skills.\n- **Big.** "
	_, err = RenderConstitution([]Card{*big})
	wantErr(t, err, fmt.Sprintf("constitution is %d chars, limit %d", len(prefix)+ConstitutionLimit+1, ConstitutionLimit))
	widePrefix := "# Taste\nFull cards: /taste-* skills.\n- **Wide.** "
	wide, err := ParseCard(card(map[string]string{"id": "wide", "title": "Wide", "always": "true"},
		strings.Repeat("é", ConstitutionLimit-len(widePrefix)-1)), "wide")
	if err != nil {
		t.Fatalf("parse wide: %v", err)
	}
	text, err = RenderConstitution([]Card{*wide})
	if err != nil {
		t.Fatalf("%d multi-byte characters are within the limit: %v", ConstitutionLimit, err)
	}
	if len(text) <= ConstitutionLimit {
		t.Fatalf("constitution is %d bytes; the case needs more bytes than the limit to prove characters are counted", len(text))
	}
}

func TestAnEnforceIdAbsentFromThePresetsFailsTheBuild(t *testing.T) {
	f := newFixture(t, map[string]string{
		"judgment/a.md": card(map[string]string{"id": "a", "enforce": "[biome:style/noEnum, biome:style/noSuchRule]"}, cardBody),
	}, map[string]any{"style": map[string]any{"noEnum": "error"}})
	_, err := Build(f.paths)
	wantErr(t, err, "enforce ids absent from presets:\na: biome:style/noSuchRule")
	f2 := newFixture(t, map[string]string{
		"judgment/a.md": card(map[string]string{"id": "a", "enforce": "[biome:style/noEnum, eslint:no-else-return]"}, cardBody),
	}, map[string]any{"style": map[string]any{"noEnum": "error"}})
	if _, err := Build(f2.paths); err != nil {
		t.Fatalf("build with known rules: %v", err)
	}
	f3 := newFixture(t, map[string]string{
		"judgment/c.md": card(map[string]string{"id": "c", "status": "candidate", "enforce": "[biome:style/noSuchRule]"}, cardBody),
	}, map[string]any{"style": map[string]any{"noEnum": "error"}})
	_, err = Build(f3.paths)
	wantErr(t, err, "enforce ids absent from presets:\nc: biome:style/noSuchRule")
}

func TestTheAgentsFragmentIsWrappedInTasteMarkers(t *testing.T) {
	f := newFixture(t, map[string]string{
		"judgment/a.md": card(map[string]string{"id": "a", "title": "Always A", "always": "true"}, cardBody),
	}, map[string]any{"style": map[string]any{"noEnum": "error"}})
	if _, err := Build(f.paths); err != nil {
		t.Fatalf("build: %v", err)
	}
	fragment := readFile(t, filepath.Join(f.paths.DistDir, "AGENTS.fragment.md"))
	constitution := readFile(t, filepath.Join(f.paths.DistDir, "constitution.md"))
	if !strings.HasPrefix(fragment, FragmentBegin) {
		t.Error("fragment does not start with begin marker")
	}
	if !strings.HasSuffix(strings.TrimRight(fragment, "\n"), FragmentEnd) {
		t.Error("fragment does not end with end marker")
	}
	if !strings.Contains(fragment, constitution) {
		t.Error("fragment does not carry the constitution")
	}
}
