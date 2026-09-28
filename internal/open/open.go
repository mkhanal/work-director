// Package open resolves file targets and opens them in the host's editor,
// printing an OSC-8 clickable link either way. It is a port of
// packages/wd/src/open.ts with the same targets, editor preference and
// output.
package open

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"wd/internal/runner"
)

// OpenTarget is a resolved file path plus an optional line.
type OpenTarget struct {
	Path string
	Line *int
}

var targetRe = regexp.MustCompile(`^(.*[^:]):(\d+)$`)

// ParseTarget resolves `<path>:<line>` against baseDir; no colon match is a
// plain path.
func ParseTarget(file, baseDir string) OpenTarget {
	if m := targetRe.FindStringSubmatch(file); m != nil {
		line, _ := strconv.Atoi(m[2])
		return OpenTarget{Path: filepath.Join(baseDir, m[1]), Line: &line}
	}
	return OpenTarget{Path: filepath.Join(baseDir, file)}
}

// Editor is the terminal host's editor: a label, the command, and the args
// for a target.
type Editor struct {
	Label string
	Cmd   string
	Args  func(OpenTarget) []string
}

// DetectEditor finds the host's editor, in order of preference: VS Code
// (reuse-window), then $VISUAL/$EDITOR.
func DetectEditor() *Editor {
	if _, err := exec.LookPath("code"); err == nil {
		return &Editor{Label: "Visual Studio Code", Cmd: "code", Args: func(t OpenTarget) []string {
			if t.Line == nil {
				return []string{"--reuse-window", t.Path}
			}
			return []string{"--reuse-window", "--goto", t.Path + ":" + strconv.Itoa(*t.Line)}
		}}
	}
	v := os.Getenv("VISUAL")
	if v == "" {
		v = os.Getenv("EDITOR")
	}
	if v != "" {
		parts := strings.Fields(v)
		cmd := parts[0]
		rest := parts[1:]
		return &Editor{Label: v, Cmd: cmd, Args: func(t OpenTarget) []string {
			return append(append([]string{}, rest...), t.Path)
		}}
	}
	return nil
}

// OpenLink renders an iTerm2/kitty-style clickable file link (OSC-8), so a
// path printed in wd output can be cmd-clicked.
func OpenLink(t OpenTarget) string {
	host, ok := os.LookupEnv("HOSTNAME")
	if !ok || host == "" {
		host = "localhost"
	}
	label := t.Path
	if t.Line != nil {
		label = t.Path + ":" + strconv.Itoa(*t.Line)
	}
	return "\x1b]8;;file://" + host + t.Path + lineFrag(t) + "\x1b\\" + label + "\x1b]8;;\x1b\\"
}

func lineFrag(t OpenTarget) string {
	if t.Line == nil {
		return ""
	}
	return "#" + strconv.Itoa(*t.Line)
}

// OpenInEditor opens the target in the editor, reporting success.
func OpenInEditor(editor Editor, t OpenTarget) (bool, error) {
	cwd := filepath.Dir(t.Path)
	if cwd == "" {
		cwd = "/"
	}
	r, err := runner.Run(append([]string{editor.Cmd}, editor.Args(t)...), cwd)
	if err != nil {
		return false, err
	}
	return r.Code == 0, nil
}

// FileLabel is the basename of the target, with the line when present.
func FileLabel(t OpenTarget) string {
	label := filepath.Base(t.Path)
	if t.Line != nil {
		label += ":" + strconv.Itoa(*t.Line)
	}
	return label
}
