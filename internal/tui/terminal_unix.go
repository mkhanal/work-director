//go:build !windows

package tui

import (
	"os"

	"golang.org/x/sys/unix"
)

// savedMode is the terminal's state before raw mode.
type savedMode = unix.Termios

// makeRaw switches in to raw mode and returns the state to restore.
func makeRaw(in, _ *os.File) (*savedMode, error) {
	fd := int(in.Fd())
	old, err := unix.IoctlGetTermios(fd, tcgets)
	if err != nil {
		return nil, err
	}
	raw := *old
	raw.Iflag &^= unix.IGNBRK | unix.BRKINT | unix.PARMRK | unix.ISTRIP | unix.INLCR | unix.IGNCR | unix.ICRNL | unix.IXON
	raw.Oflag &^= unix.OPOST
	raw.Lflag &^= unix.ECHO | unix.ECHONL | unix.ICANON | unix.ISIG | unix.IEXTEN
	raw.Cflag &^= unix.CSIZE | unix.PARENB
	raw.Cflag |= unix.CS8
	raw.Cc[unix.VMIN] = 1
	raw.Cc[unix.VTIME] = 0
	if err := unix.IoctlSetTermios(fd, tcsets, &raw); err != nil {
		return nil, err
	}
	return old, nil
}

// pollInput switches in between blocking reads and reads that give up after
// 100ms.
func pollInput(in *os.File, on bool) error {
	fd := int(in.Fd())
	cur, err := unix.IoctlGetTermios(fd, tcgets)
	if err != nil {
		return err
	}
	if on {
		cur.Cc[unix.VMIN] = 0
		cur.Cc[unix.VTIME] = 1
	} else {
		cur.Cc[unix.VMIN] = 1
		cur.Cc[unix.VTIME] = 0
	}
	return unix.IoctlSetTermios(fd, tcsets, cur)
}

func size(out *os.File) (int, int, error) {
	ws, err := unix.IoctlGetWinsize(int(out.Fd()), unix.TIOCGWINSZ)
	if err != nil {
		return 0, 0, err
	}
	return int(ws.Col), int(ws.Row), nil
}

func restoreMode(in *os.File, old *savedMode) error {
	return unix.IoctlSetTermios(int(in.Fd()), tcsets, old)
}
