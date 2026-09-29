package open

import "testing"

func TestParseTargetKeepsAbsolutePaths(t *testing.T) {
	for _, c := range []struct {
		file, path string
		line       int
	}{
		{"/etc/hosts", "/etc/hosts", 0},
		{"/etc/hosts:3", "/etc/hosts", 3},
		{"src/a.go", "/base/src/a.go", 0},
		{"src/a.go:12", "/base/src/a.go", 12},
	} {
		got := ParseTarget(c.file, "/base")
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
