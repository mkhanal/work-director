package project

import "testing"

func TestParseProjectExpandsOnlyTheUsersHome(t *testing.T) {
	t.Setenv("HOME", "/home/me")
	for raw, want := range map[string]string{
		"~":        "/home/me",
		"~/repo":   "/home/me/repo",
		"~bob/x":   "~bob/x",
		"/abs/~/x": "/abs/~/x",
	} {
		p, err := ParseProject("---\npath: "+raw+"\nrunner: claude\n---\n", "/w/proj.md")
		if err != nil {
			t.Fatalf("%s: %v", raw, err)
		}
		if p.Path != want {
			t.Fatalf("path %q = %q, want %q", raw, p.Path, want)
		}
	}
}

func TestExpandHomeFailsOnlyWhenItNeedsAnUnknownHome(t *testing.T) {
	t.Setenv("HOME", "")
	for _, raw := range []string{"~", "~/repo"} {
		if got, err := ExpandHome(raw); err == nil {
			t.Fatalf("%s expanded to %q with no home, want an error", raw, got)
		}
	}
	for _, raw := range []string{"/abs/x", "~bob/x", "rel/~"} {
		got, err := ExpandHome(raw)
		if err != nil || got != raw {
			t.Fatalf("%s = %q, %v, want it unchanged", raw, got, err)
		}
	}
}

func TestParseProjectTreatsAnEmptyAgentAsUnset(t *testing.T) {
	p, err := ParseProject("---\npath: /repo\nrunner: opencode\nagent: \nmodel: \n---\n", "/w/proj.md")
	if err != nil {
		t.Fatal(err)
	}
	if p.Agent != nil || p.Model != nil {
		t.Fatalf("agent/model = %v/%v, want both unset", p.Agent, p.Model)
	}
	p, err = ParseProject("---\npath: /repo\nrunner: opencode\nagent: build\n---\n", "/w/proj.md")
	if err != nil {
		t.Fatal(err)
	}
	if p.Agent == nil || *p.Agent != "build" {
		t.Fatalf("agent = %v, want build", p.Agent)
	}
}
