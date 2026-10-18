package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"time"

	"wd/internal/crash"
)

// crashes lists what the apps and wd serve left behind when they died, or
// shows one in full.
func (c *Cli) crashes(rest []string) error {
	switch len(rest) {
	case 0:
		list, err := crash.List(c.Home)
		if err != nil {
			return err
		}
		if list == nil {
			list = []crash.Crash{}
		}
		return c.out(list, crashList(list))
	case 1:
		d, err := crash.Load(c.Home, rest[0])
		if errors.Is(err, crash.ErrUnknown) {
			return fail("no crash %q: wd crashes lists them", rest[0])
		}
		if err != nil {
			return err
		}
		return c.out(d, crashText(d))
	default:
		return fail("usage: %s", help["crashes"].usage)
	}
}

func crashList(list []crash.Crash) string {
	if len(list) == 0 {
		return "No crashes recorded."
	}
	var b strings.Builder
	for _, x := range list {
		fmt.Fprintf(&b, "%s  %s  %s: %s\n", x.ID, x.At.Local().Format("2006-01-02 15:04"), appName(x.App), x.Reason)
	}
	b.WriteString("\nwd crashes <id> shows one.")
	return b.String()
}

func appName(app string) string {
	switch app {
	case "macos":
		return "Work Director for macOS"
	case "linux":
		return "Work Director for Linux"
	default:
		return app
	}
}

func crashText(d crash.Detail) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s crashed at %s", appName(d.App), d.At.Local().Format("2006-01-02 15:04:05"))
	if d.Build != "" {
		fmt.Fprintf(&b, ", build %s", d.Build)
	}
	fmt.Fprintf(&b, ".\nWhy: %s\n", d.Reason)
	if d.Screen != nil {
		on := d.Screen.View
		if d.Screen.Work != nil {
			on = *d.Screen.Work + ", " + on
		}
		fmt.Fprintf(&b, "On screen: %s\n", on)
	}
	if len(d.Crumbs) > 0 {
		b.WriteString("\nLast things done, oldest first:\n")
		for _, c := range d.Crumbs {
			fmt.Fprintf(&b, "  %s  %s", c.At.Local().Format("15:04:05"), c.What)
			if c.Times > 1 {
				fmt.Fprintf(&b, " (%d times)", c.Times)
			}
			b.WriteString("\n")
		}
	}
	if d.Exception != nil && len(d.Exception.Stack) > 0 {
		fmt.Fprintf(&b, "\nWhere it was thrown:\n")
		for _, l := range d.Exception.Stack {
			fmt.Fprintf(&b, "  %s\n", l)
		}
	}
	if s := d.System; s != nil {
		fmt.Fprintf(&b, "\nThe system's crash report: %s\n", s.Signal)
		for _, m := range s.Messages {
			fmt.Fprintf(&b, "  %s\n", m)
		}
		if d.Exception == nil && len(s.Thrown) > 0 {
			b.WriteString("\nWhere it was thrown, from the system's report:\n")
			for _, l := range s.Thrown {
				fmt.Fprintf(&b, "  %s\n", l)
			}
		}
		b.WriteString("\nThe crashing thread, innermost first:\n")
		for i, f := range s.Thread {
			fmt.Fprintf(&b, "  %-3d %s\n", i, f)
		}
	}
	fmt.Fprintf(&b, "\nKept in %s", d.Path)
	return b.String()
}

// keepCrashOutput sends what the Go runtime prints as this process dies, an
// unrecovered panic or a fatal error, to a log under $WD_HOME/logs as well as
// stderr, where wd crashes finds it. The process still dies as it would. The
// log is removed by the returned func on a clean return, so only crashes stay.
func (c *Cli) keepCrashOutput() (func() error, error) {
	path := crash.ServeLog(c.Home, time.Now(), os.Getpid())
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	f, err := os.Create(path)
	if err != nil {
		return nil, err
	}
	build := Version
	if Commit != "" {
		build += " (" + Commit + ")"
	}
	if _, err := fmt.Fprintf(f, "wd %s\n", build); err != nil {
		f.Close()
		return nil, err
	}
	// SetCrashOutput holds a duplicate of f's descriptor.
	if err := debug.SetCrashOutput(f, debug.CrashOptions{}); err != nil {
		f.Close()
		return nil, err
	}
	if err := f.Close(); err != nil {
		return nil, err
	}
	return func() error {
		if err := debug.SetCrashOutput(nil, debug.CrashOptions{}); err != nil {
			return err
		}
		return os.Remove(path)
	}, nil
}
