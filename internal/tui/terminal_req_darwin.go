//go:build darwin

package tui

import "golang.org/x/sys/unix"

// termios requests for raw mode.
const (
	tcgets = uint(unix.TIOCGETA)
	tcsets = uint(unix.TIOCSETA)
)
