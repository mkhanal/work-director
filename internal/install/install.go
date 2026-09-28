// Package install probes and installs the director's runtime dependencies:
// the external commands the static wd binary shells out to. The binary
// itself has no runtime dependencies — it is built with CGO disabled — so
// this is the whole of what "setup" means for the director.
package install

import (
	"os/exec"
	"runtime"
)

// Dep is one runtime dependency's probed status: present with the resolved
// path, or missing with the command that installs it.
type Dep struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Path    string `json:"path"`
	Install string `json:"install"`
}

// The probed statuses.
const (
	StatusPresent = "present"
	StatusMissing = "missing"
)

// Names are the runtime dependencies wd probes, in report order: git for
// worktrees and merges, then the four runner CLIs.
var Names = []string{"git", "claude", "opencode", "codex", "ao"}

// Probe checks every dependency and returns its status.
func Probe() []Dep {
	deps := make([]Dep, 0, len(Names))
	for _, name := range Names {
		deps = append(deps, probe(name))
	}
	return deps
}

// probe resolves one dependency against the current PATH.
func probe(name string) Dep {
	path, err := exec.LookPath(name)
	if err != nil {
		return Dep{Name: name, Status: StatusMissing, Install: InstallCommand(name)}
	}
	return Dep{Name: name, Status: StatusPresent, Path: path}
}

// InstallCommand returns the shell command that installs name, or "" when
// the director does not know one.
func InstallCommand(name string) string {
	switch name {
	case "git":
		return gitInstallCommand()
	case "claude":
		return "npm install -g @anthropic-ai/claude-code"
	case "opencode":
		return "npm install -g opencode-ai"
	case "codex":
		return "npm install -g @openai/codex"
	case "ao":
		return "npm install -g @aoagents/ao"
	}
	return ""
}

// gitInstallCommand names git's installer per platform: Homebrew on macOS,
// the system package manager on Linux.
func gitInstallCommand() string {
	if runtime.GOOS == "darwin" {
		return "brew install git"
	}
	for _, pm := range []struct{ bin, cmd string }{
		{"apt-get", "sudo apt-get install -y git"},
		{"dnf", "sudo dnf install -y git"},
		{"apk", "sudo apk add git"},
	} {
		if _, err := exec.LookPath(pm.bin); err == nil {
			return pm.cmd
		}
	}
	return ""
}

// Install runs the install command for every missing dependency, using run
// to execute each command, then re-probes: a dependency whose command is
// unknown or failed stays missing.
func Install(run func(cmd string)) []Dep {
	for _, dep := range Probe() {
		if dep.Status == StatusMissing && dep.Install != "" {
			run(dep.Install)
		}
	}
	return Probe()
}

// Missing names the dependencies that are not present, in report order.
func Missing(deps []Dep) []string {
	var names []string
	for _, dep := range deps {
		if dep.Status != StatusPresent {
			names = append(names, dep.Name)
		}
	}
	return names
}
