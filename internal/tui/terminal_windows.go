//go:build windows

package tui

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

// savedMode is the console's input and output modes before raw mode.
type savedMode struct {
	inH, outH windows.Handle
	in, out   uint32
}

// makeRaw turns off line input, echo and Ctrl-C handling on the console's
// input and asks both sides for the escape sequences a Unix terminal speaks,
// so the same decoder reads keys and the same frames draw.
func makeRaw(in, out *os.File) (*savedMode, error) {
	ih, oh := windows.Handle(in.Fd()), windows.Handle(out.Fd())
	s := savedMode{inH: ih, outH: oh}
	if err := windows.GetConsoleMode(ih, &s.in); err != nil {
		return nil, err
	}
	if err := windows.GetConsoleMode(oh, &s.out); err != nil {
		return nil, err
	}
	raw := s.in &^ (windows.ENABLE_LINE_INPUT | windows.ENABLE_ECHO_INPUT | windows.ENABLE_PROCESSED_INPUT)
	if err := windows.SetConsoleMode(ih, raw|windows.ENABLE_VIRTUAL_TERMINAL_INPUT); err != nil {
		return nil, err
	}
	if err := windows.SetConsoleMode(oh, s.out|windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING); err != nil {
		return nil, err
	}
	return &s, nil
}

// pollInput has nothing to switch: a console read cannot time out, so a lone
// Escape is read once the next key arrives.
func pollInput(*os.File, bool) error { return nil }

func size(out *os.File) (int, int, error) {
	var info windows.ConsoleScreenBufferInfo
	if err := windows.GetConsoleScreenBufferInfo(windows.Handle(out.Fd()), &info); err != nil {
		return 0, 0, err
	}
	return int(info.Window.Right - info.Window.Left + 1), int(info.Window.Bottom - info.Window.Top + 1), nil
}

func restoreMode(_ *os.File, old *savedMode) error {
	return errors.Join(windows.SetConsoleMode(old.inH, old.in), windows.SetConsoleMode(old.outH, old.out))
}
