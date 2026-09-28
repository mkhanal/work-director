package tui

import (
	"os"
	"strings"
	"unicode/utf8"

	"golang.org/x/sys/unix"
)

// KeyKind is the kind of one input event.
type KeyKind int

const (
	KeyRune KeyKind = iota
	KeyEnter
	KeyBackspace
	KeyEscape
	KeyUp
	KeyDown
	KeyLeft
	KeyRight
	KeyCtrlC
)

// Key is one input event: a rune or a control key.
type Key struct {
	Kind KeyKind
	Rune rune
}

// terminal is the real terminal: raw input on in, ANSI frames on out.
type terminal struct {
	in   *os.File
	out  *os.File
	old  *unix.Termios
	keys chan Key
	done chan struct{}
}

// NewTerminal opens the TUI's terminal and starts reading keys.
func NewTerminal(in, out *os.File) (Terminal, error) {
	t := &terminal{in: in, out: out, keys: make(chan Key, 32), done: make(chan struct{})}
	if err := t.raw(); err != nil {
		return nil, err
	}
	go t.read()
	return t, nil
}

// raw switches the input to raw mode, saving the previous state.
func (t *terminal) raw() error {
	fd := int(t.in.Fd())
	old, err := unix.IoctlGetTermios(fd, tcgets)
	if err != nil {
		return err
	}
	t.old = old
	raw := *old
	raw.Iflag &^= unix.IGNBRK | unix.BRKINT | unix.PARMRK | unix.ISTRIP | unix.INLCR | unix.IGNCR | unix.ICRNL | unix.IXON
	raw.Oflag &^= unix.OPOST
	raw.Lflag &^= unix.ECHO | unix.ECHONL | unix.ICANON | unix.ISIG | unix.IEXTEN
	raw.Cflag &^= unix.CSIZE | unix.PARENB
	raw.Cflag |= unix.CS8
	raw.Cc[unix.VMIN] = 1
	raw.Cc[unix.VTIME] = 0
	return unix.IoctlSetTermios(fd, tcsets, &raw)
}

// read parses input bytes into keys until the terminal closes.
func (t *terminal) read() {
	b := make([]byte, 1)
	for {
		if !t.readByte(b) {
			return
		}
		switch b[0] {
		case 0x1b:
			t.escape()
		case '\r', '\n':
			t.send(Key{Kind: KeyEnter})
		case 0x03:
			t.send(Key{Kind: KeyCtrlC})
		case 0x7f, 0x08:
			t.send(Key{Kind: KeyBackspace})
		default:
			if b[0] >= 0x20 && b[0] < 0x7f {
				t.send(Key{Kind: KeyRune, Rune: rune(b[0])})
				continue
			}
			if b[0] >= 0x80 {
				t.readUTF8(b[0])
			}
		}
	}
}

// readUTF8 reads the rest of a multi-byte rune.
func (t *terminal) readUTF8(first byte) {
	n := runeLen(first)
	buf := make([]byte, n)
	buf[0] = first
	for i := 1; i < n; i++ {
		if !t.readByte(buf[i:]) {
			return
		}
	}
	if r, _ := utf8.DecodeRune(buf); r != utf8.RuneError {
		t.send(Key{Kind: KeyRune, Rune: r})
	}
}

// runeLen is the length of a UTF-8 sequence by its leading byte.
func runeLen(b byte) int {
	switch {
	case b >= 0xf0:
		return 4
	case b >= 0xe0:
		return 3
	default:
		return 2
	}
}

// escape reads the rest of an escape sequence with a short deadline: arrows
// and friends, or a bare Escape.
func (t *terminal) escape() {
	t.setDeadline(true)
	defer t.setDeadline(false)
	b := make([]byte, 1)
	if !t.readByte(b) || b[0] != '[' {
		t.send(Key{Kind: KeyEscape})
		return
	}
	if !t.readByte(b) {
		t.send(Key{Kind: KeyEscape})
		return
	}
	var k Key
	switch b[0] {
	case 'A':
		k = Key{Kind: KeyUp}
	case 'B':
		k = Key{Kind: KeyDown}
	case 'C':
		k = Key{Kind: KeyRight}
	case 'D':
		k = Key{Kind: KeyLeft}
	default:
		k = Key{Kind: KeyEscape}
	}
	t.send(k)
}

// setDeadline switches the input between blocking reads and 100ms reads.
func (t *terminal) setDeadline(on bool) {
	cur, err := unix.IoctlGetTermios(int(t.in.Fd()), tcgets)
	if err != nil {
		return
	}
	if on {
		cur.Cc[unix.VMIN] = 0
		cur.Cc[unix.VTIME] = 1
	} else {
		cur.Cc[unix.VMIN] = 1
		cur.Cc[unix.VTIME] = 0
	}
	unix.IoctlSetTermios(int(t.in.Fd()), tcsets, cur)
}

// readByte reads one byte, reporting whether one arrived.
func (t *terminal) readByte(b []byte) bool {
	n, err := t.in.Read(b)
	if err != nil || n == 0 {
		return false
	}
	return true
}

// send pushes a key to the app, dropping it when the terminal is closing.
func (t *terminal) send(k Key) {
	select {
	case t.keys <- k:
	case <-t.done:
	}
}

// Size returns the terminal's width and height in cells.
func (t *terminal) Size() (int, int, error) {
	ws, err := unix.IoctlGetWinsize(int(t.out.Fd()), unix.TIOCGWINSZ)
	if err != nil {
		return 0, 0, err
	}
	return int(ws.Col), int(ws.Row), nil
}

// Keys returns the channel of input events.
func (t *terminal) Keys() <-chan Key { return t.keys }

// Write draws one frame: cursor home, the lines, then clear to the end.
func (t *terminal) Write(lines []string) error {
	w, _, err := t.Size()
	if err != nil {
		return err
	}
	var b strings.Builder
	b.WriteString("\x1b[H")
	for _, l := range lines {
		b.WriteString(fit(l, w))
		b.WriteString("\r\n")
	}
	b.WriteString("\x1b[J")
	_, err = t.out.WriteString(b.String())
	return err
}

// Close restores the terminal and stops reading.
func (t *terminal) Close() error {
	if t.old != nil {
		unix.IoctlSetTermios(int(t.in.Fd()), tcsets, t.old)
		t.old = nil
	}
	close(t.done)
	return t.in.Close()
}

// fit truncates one line to the terminal's width.
func fit(l string, w int) string {
	if w <= 0 {
		return ""
	}
	r := []rune(l)
	if len(r) <= w {
		return l
	}
	return string(r[:w])
}
