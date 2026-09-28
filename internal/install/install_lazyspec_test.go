package install

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Every requirement in install.lazyspec.md is married to a test here. The
// tests put fake dependency CLIs on PATH (the same trick the CLI fixture
// uses) so probe results are deterministic regardless of the machine.

// useFakeDeps puts executable fakes for every dependency on PATH and
// restores the original PATH after the test.
func useFakeDeps(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	for _, name := range Names {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
			t.Fatalf("write fake %s: %v", name, err)
		}
	}
	orig := os.Getenv("PATH")
	t.Cleanup(func() { os.Setenv("PATH", orig) })
	os.Setenv("PATH", dir)
}

func TestSetupProbesTheRuntimeDependencies(t *testing.T) {
	useFakeDeps(t)
	for _, dep := range Probe() {
		if dep.Status != StatusPresent {
			t.Errorf("%s: status = %q, want %q", dep.Name, dep.Status, StatusPresent)
		}
		if dep.Path == "" {
			t.Errorf("%s: present but no path", dep.Name)
		}
		if dep.Install != "" {
			t.Errorf("%s: present but carries install command %q", dep.Name, dep.Install)
		}
	}
	// With the fakes gone, every dependency is missing with its install command.
	os.Setenv("PATH", t.TempDir())
	for _, dep := range Probe() {
		if dep.Status != StatusMissing {
			t.Errorf("%s: status = %q, want %q", dep.Name, dep.Status, StatusMissing)
		}
		if dep.Path != "" {
			t.Errorf("%s: missing but carries path %q", dep.Name, dep.Path)
		}
		if dep.Install == "" {
			t.Errorf("%s: missing but no install command", dep.Name)
		}
	}
}

func TestInstallRunsEachMissingDependenciesCommand(t *testing.T) {
	os.Setenv("PATH", t.TempDir())
	var ran []string
	report := Install(func(cmd string) {
		ran = append(ran, cmd)
	})
	// Every missing dependency's command ran, in report order.
	want := make([]string, 0, len(Names))
	for _, name := range Names {
		want = append(want, InstallCommand(name))
	}
	if strings.Join(ran, "\n") != strings.Join(want, "\n") {
		t.Errorf("ran %v, want %v", ran, want)
	}
	// The runner CLIs install through npm; git through the platform's
	// package manager.
	for _, name := range []string{"claude", "opencode", "codex", "ao"} {
		if !strings.HasPrefix(InstallCommand(name), "npm install -g ") {
			t.Errorf("%s: install command %q is not an npm global install", name, InstallCommand(name))
		}
	}
	if !strings.Contains(InstallCommand("git"), "git") {
		t.Errorf("git: install command %q does not name git", InstallCommand("git"))
	}
	// A no-op run installs nothing: the re-probe still finds every
	// dependency missing.
	if len(Missing(report)) != len(Names) {
		t.Errorf("after install, missing = %v, want all %d", Missing(report), len(Names))
	}
}
