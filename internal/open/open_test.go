package open

import (
	"path/filepath"
	"testing"
)

func TestParseTargetKeepsAbsolutePaths(t *testing.T) {
	// Absolute as the system running the test spells it.
	base := filepath.Join(t.TempDir(), "base")
	abs := filepath.Join(t.TempDir(), "etc", "hosts")
	for _, c := range []struct {
		file, path string
		line       int
	}{
		{abs, abs, 0},
		{abs + ":3", abs, 3},
		{"src/a.go", filepath.Join(base, "src", "a.go"), 0},
		{"src/a.go:12", filepath.Join(base, "src", "a.go"), 12},
	} {
		got := ParseTarget(c.file, base)
		if got.Path != c.path {
			t.Fatalf("%s: path = %q, want %q", c.file, got.Path, c.path)
		}
		if (c.line == 0) != (got.Line == nil) || (got.Line != nil && *got.Line != c.line) {
			t.Fatalf("%s: line = %v, want %d", c.file, got.Line, c.line)
		}
	}
}

func TestDetectEditorFailsOnABlankEditorVariable(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	t.Setenv("VISUAL", "  ")
	if _, err := DetectEditor(); err == nil {
		t.Fatal("blank VISUAL was accepted")
	}
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", "vim -p")
	e, err := DetectEditor()
	if err != nil || e == nil || e.Cmd != "vim" {
		t.Fatalf("editor = %+v, %v, want vim", e, err)
	}
	t.Setenv("EDITOR", "")
	if e, err := DetectEditor(); err != nil || e != nil {
		t.Fatalf("editor = %+v, %v, want none", e, err)
	}
}
