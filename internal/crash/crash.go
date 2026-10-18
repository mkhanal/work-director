// Package crash reads what Work Director leaves behind when it dies: the notes
// the desktop apps write under $WD_HOME/crashes, the system crash report a
// macOS app's note is paired with, and the panic output of wd serve under
// $WD_HOME/logs.
package crash

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// Note is note.json in a crash's folder, as the apps write it.
type Note struct {
	App   string    `json:"app"`
	At    time.Time `json:"at"`
	Build string    `json:"build"`
	PID   int       `json:"pid"`
	// Exception is what was thrown; nil when the app ended without one it
	// could catch (a Swift trap, a signal, a kill).
	Exception *Exception `json:"exception"`
	Screen    Screen     `json:"screen"`
	Crumbs    []Crumb    `json:"crumbs"`
	// Report is the system crash report's file name in the same folder,
	// empty when the system wrote none.
	Report string `json:"report"`
}

// Exception is an uncaught exception or a panic, with its stack as the
// process printed it.
type Exception struct {
	Name   string   `json:"name"`
	Reason string   `json:"reason"`
	Stack  []string `json:"stack"`
}

// Screen is what the app showed: the work open, if any, and which view of it.
type Screen struct {
	Work *string `json:"work"`
	View string  `json:"view"`
}

// Crumb is one thing the app did, Times in a row.
type Crumb struct {
	At    time.Time `json:"at"`
	What  string    `json:"what"`
	Times int       `json:"times"`
}

// Crash is one entry of the list: a note's folder, or a wd serve panic log.
type Crash struct {
	ID     string    `json:"id"`
	App    string    `json:"app"`
	At     time.Time `json:"at"`
	Build  string    `json:"build"`
	Reason string    `json:"reason"`
	Path   string    `json:"path"`
}

// Detail is one crash with everything kept about it.
type Detail struct {
	Crash
	Exception *Exception `json:"exception"`
	Screen    *Screen    `json:"screen"`
	Crumbs    []Crumb    `json:"crumbs"`
	// System is the paired system crash report, nil when there is none.
	System *Report `json:"system"`
}

// servePrefix names a wd serve crash log, and the id it is listed under.
const servePrefix = "serve-"

// ServeLog is the file a wd serve started at at with pid writes its crash
// output to, after a first line naming its build.
func ServeLog(home string, at time.Time, pid int) string {
	return filepath.Join(home, "logs", fmt.Sprintf("%s%s-%d.log", servePrefix, at.UTC().Format("20060102-150405"), pid))
}

// List is every crash under home, newest first.
func List(home string) ([]Crash, error) {
	var out []Crash
	dirs, err := os.ReadDir(filepath.Join(home, "crashes"))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	for _, d := range dirs {
		if !d.IsDir() {
			continue
		}
		detail, err := load(home, d.Name())
		if err != nil {
			return nil, err
		}
		out = append(out, detail.Crash)
	}
	logs, err := filepath.Glob(filepath.Join(home, "logs", servePrefix+"*.log"))
	if err != nil {
		return nil, err
	}
	for _, path := range logs {
		detail, ok, err := serveCrash(path)
		if err != nil {
			return nil, err
		}
		if ok {
			out = append(out, detail.Crash)
		}
	}
	slices.SortFunc(out, func(a, b Crash) int { return b.At.Compare(a.At) })
	return out, nil
}

// ErrUnknown is an id no crash has.
var ErrUnknown = errors.New("no crash with that id")

// Load is the crash id under home.
func Load(home, id string) (Detail, error) {
	if id == "" || strings.ContainsAny(id, `/\`) || id == "." || id == ".." {
		return Detail{}, ErrUnknown
	}
	if strings.HasPrefix(id, servePrefix) {
		detail, ok, err := serveCrash(filepath.Join(home, "logs", id+".log"))
		if errors.Is(err, fs.ErrNotExist) || (err == nil && !ok) {
			return Detail{}, ErrUnknown
		}
		return detail, err
	}
	detail, err := load(home, id)
	if errors.Is(err, fs.ErrNotExist) {
		return Detail{}, ErrUnknown
	}
	return detail, err
}

func load(home, id string) (Detail, error) {
	dir := filepath.Join(home, "crashes", id)
	data, err := os.ReadFile(filepath.Join(dir, "note.json"))
	if err != nil {
		return Detail{}, err
	}
	var n Note
	if err := json.Unmarshal(data, &n); err != nil {
		return Detail{}, fmt.Errorf("%s: %w", filepath.Join(dir, "note.json"), err)
	}
	d := Detail{
		Crash:     Crash{ID: id, App: n.App, At: n.At, Build: n.Build, Path: dir},
		Exception: n.Exception,
		Screen:    &n.Screen,
		Crumbs:    n.Crumbs,
	}
	if d.Crumbs == nil {
		d.Crumbs = []Crumb{}
	}
	if n.Report != "" {
		data, err := os.ReadFile(filepath.Join(dir, n.Report))
		if err != nil {
			return Detail{}, err
		}
		r, err := ParseReport(data)
		if err != nil {
			return Detail{}, fmt.Errorf("%s: %w", filepath.Join(dir, n.Report), err)
		}
		d.System = &r
	}
	d.Reason = reason(d)
	return d, nil
}

// reason is the crash in one line: what was thrown, else what the system
// says ended the process, else that nothing says why.
func reason(d Detail) string {
	switch {
	case d.Exception != nil:
		return d.Exception.Name + ": " + d.Exception.Reason
	case d.System != nil && len(d.System.Messages) > 0:
		return d.System.Messages[0]
	case d.System != nil:
		return d.System.Signal
	default:
		return "ended without a clean exit and left no crash report: it was killed or force-quit"
	}
}

// serveCrash reads a wd serve crash log: its first line names the build, and
// anything after it is what the Go runtime wrote as the process died. False
// when there is nothing after it: a serve still running, or one killed
// without a word.
func serveCrash(path string) (Detail, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Detail{}, false, err
	}
	text := strings.TrimSpace(string(data))
	if text == "" {
		return Detail{}, false, nil
	}
	st, err := os.Stat(path)
	if err != nil {
		return Detail{}, false, err
	}
	lines := strings.Split(text, "\n")
	build := ""
	if b, ok := strings.CutPrefix(lines[0], "wd "); ok {
		build, lines = b, lines[1:]
	}
	if len(lines) == 0 {
		return Detail{}, false, nil
	}
	why := lines[0]
	for _, l := range lines {
		if strings.HasPrefix(l, "panic: ") || strings.HasPrefix(l, "fatal error: ") {
			why = l
			break
		}
	}
	id := strings.TrimSuffix(filepath.Base(path), ".log")
	return Detail{
		Crash:     Crash{ID: id, App: "wd serve", At: st.ModTime().UTC(), Build: build, Reason: why, Path: path},
		Exception: &Exception{Name: "Go runtime", Reason: why, Stack: lines},
		Crumbs:    []Crumb{},
	}, true, nil
}
