//go:build linux

package tui

import "golang.org/x/sys/unix"

// termios requests for raw mode.
const (
	tcgets = uint(unix.TCGETS)
	tcsets = uint(unix.TCSETS)
)
