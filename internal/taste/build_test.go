package taste

import (
	"strings"
	"testing"
)

func TestLoadPresetsRejectsAMalformedBiomeGroup(t *testing.T) {
	f := newFixture(t, map[string]string{}, map[string]any{"recommended": true, "style": []string{"noEnum"}})
	_, err := LoadPresets(f.paths.PresetsDir)
	if err == nil || !strings.Contains(err.Error(), "style") {
		t.Fatalf("err = %v, want a failure naming the style group", err)
	}
	ok := newFixture(t, map[string]string{}, map[string]any{"recommended": true, "style": map[string]any{"noEnum": "error"}})
	p, err := LoadPresets(ok.paths.PresetsDir)
	if err != nil {
		t.Fatalf("load presets: %v", err)
	}
	if !p.Biome["style/noEnum"] || len(p.Biome) != 1 {
		t.Fatalf("biome rules = %v, want only style/noEnum", p.Biome)
	}
}
