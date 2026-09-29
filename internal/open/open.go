// Package open resolves file targets and opens them in the host's editor,
// printing an OSC-8 clickable link either way.
package open

import (
	"fmt"
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

// ParseTarget resolves a relative `<path>:<line>` against baseDir; an
// absolute path stays as given, and no colon match is a plain path.
func ParseTarget(file, baseDir string) OpenTarget {
	resolve := func(p string) string {
		if filepath.IsAbs(p) {
			return p
		}
		return filepath.Join(baseDir, p)
	}
	if m := targetRe.FindStringSubmatch(file); m != nil {
		line, _ := strconv.Atoi(m[2])
		return OpenTarget{Path: resolve(m[1]), Line: &line}
	}
	return OpenTarget{Path: resolve(file)}
}

// Editor is the terminal host's editor: a label, the command, and the args
// for a target.
type Editor struct {
	Label string
	Cmd   string
	Args  func(OpenTarget) []string
}

// DetectEditor finds the host's editor, in order of preference: VS Code
// (reuse-window), then $VISUAL/$EDITOR. No editor is nil; a variable set to
// only whitespace is an error.
func DetectEditor() (*Editor, error) {
	if _, err := exec.LookPath("code"); err == nil {
		return &Editor{Label: "Visual Studio Code", Cmd: "code", Args: func(t OpenTarget) []string {
			if t.Line == nil {
				return []string{"--reuse-window", t.Path}
			}
			return []string{"--reuse-window", "--goto", t.Path + ":" + strconv.Itoa(*t.Line)}
		}}, nil
	}
	name, v := "VISUAL", os.Getenv("VISUAL")
	if v == "" {
		name, v = "EDITOR", os.Getenv("EDITOR")
	}
	if v == "" {
		return nil, nil
	}
	parts := strings.Fields(v)
	if len(parts) == 0 {
		return nil, fmt.Errorf("$%s is blank; set it to an editor command or unset it", name)
	}
	rest := parts[1:]
	return &Editor{Label: v, Cmd: parts[0], Args: func(t OpenTarget) []string {
		return append(append([]string{}, rest...), t.Path)
	}}, nil
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
	r, err := runner.Run(append([]string{editor.Cmd}, editor.Args(t)...), filepath.Dir(t.Path))
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
