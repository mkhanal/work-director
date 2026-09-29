package tui

import (
	"errors"
	"io"
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
	// err is why input stopped; written before keys closes.
	err error
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

// read parses input into keys until input fails, then closes keys. A failure
// caused by Close is not an error.
func (t *terminal) read() {
	err := t.decode()
	select {
	case <-t.done:
	default:
		t.err = err
	}
	close(t.keys)
}

// decode parses input bytes into keys until a read fails.
func (t *terminal) decode() error {
	for {
		b, err := t.readByte()
		if err != nil {
			return err
		}
		switch b {
		case 0x1b:
			err = t.escape()
		case '\r', '\n':
			t.send(Key{Kind: KeyEnter})
		case 0x03:
			t.send(Key{Kind: KeyCtrlC})
		case 0x7f, 0x08:
			t.send(Key{Kind: KeyBackspace})
		default:
			if b >= 0x20 && b < 0x7f {
				t.send(Key{Kind: KeyRune, Rune: rune(b)})
			} else if b >= 0x80 {
				err = t.readUTF8(b)
			}
		}
		if err != nil {
			return err
		}
	}
}

// readUTF8 reads the rest of a multi-byte rune.
func (t *terminal) readUTF8(first byte) error {
	buf := []byte{first}
	for len(buf) < runeLen(first) {
		b, err := t.readByte()
		if err != nil {
			return err
		}
		buf = append(buf, b)
	}
	if r, _ := utf8.DecodeRune(buf); r != utf8.RuneError {
		t.send(Key{Kind: KeyRune, Rune: r})
	}
	return nil
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

// escape reads the rest of an escape sequence with a short deadline, so a
// bare Escape is told apart from the start of a sequence.
func (t *terminal) escape() error {
	if err := t.setDeadline(true); err != nil {
		return err
	}
	var readErr error
	k, ok := escapeKey(func() (byte, bool) {
		if readErr != nil {
			return 0, false
		}
		b, err := t.readByte()
		// With the deadline on, a timeout reads as io.EOF.
		if err != nil && !errors.Is(err, io.EOF) {
			readErr = err
		}
		return b, err == nil
	})
	if err := errors.Join(readErr, t.setDeadline(false)); err != nil {
		return err
	}
	if ok {
		t.send(k)
	}
	return nil
}

// escapeKey parses the bytes after ESC; next reports false when no byte
// arrives in time. A lone ESC, or ESC followed by anything but '[', is
// Escape. A CSI sequence (ESC [, parameter bytes 0x30–0x3F, intermediate
// bytes 0x20–0x2F, a final byte 0x40–0x7E) is consumed whole: final A–D are
// the arrows, anything else — or a sequence cut short — yields no key.
func escapeKey(next func() (byte, bool)) (Key, bool) {
	b, ok := next()
	if !ok || b != '[' {
		return Key{Kind: KeyEscape}, true
	}
	if b, ok = next(); !ok {
		return Key{Kind: KeyEscape}, true
	}
	for b >= 0x20 && b <= 0x3f {
		if b, ok = next(); !ok {
			return Key{}, false
		}
	}
	switch b {
	case 'A':
		return Key{Kind: KeyUp}, true
	case 'B':
		return Key{Kind: KeyDown}, true
	case 'C':
		return Key{Kind: KeyRight}, true
	case 'D':
		return Key{Kind: KeyLeft}, true
	}
	return Key{}, false
}

// setDeadline switches the input between blocking reads and 100ms reads.
func (t *terminal) setDeadline(on bool) error {
	fd := int(t.in.Fd())
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

// readByte reads one byte.
func (t *terminal) readByte() (byte, error) {
	var b [1]byte
	n, err := t.in.Read(b[:])
	if n == 1 {
		return b[0], nil
	}
	if err == nil {
		err = io.EOF
	}
	return 0, err
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

// Err is why input stopped; valid once Keys is closed.
func (t *terminal) Err() error { return t.err }

// Write draws one frame: cursor home, the lines, then clear to the end. No
// newline follows the last line: a frame as tall as the terminal must not
// scroll it.
func (t *terminal) Write(lines []string) error {
	w, _, err := t.Size()
	if err != nil {
		return err
	}
	var b strings.Builder
	b.WriteString("\x1b[H")
	for i, l := range lines {
		if i > 0 {
			b.WriteString("\r\n")
		}
		b.WriteString(fit(l, w))
	}
	b.WriteString("\x1b[J")
	_, err = t.out.WriteString(b.String())
	return err
}

// Close restores the terminal and stops reading.
func (t *terminal) Close() error {
	var restore error
	if t.old != nil {
		restore = unix.IoctlSetTermios(int(t.in.Fd()), tcsets, t.old)
		t.old = nil
	}
	close(t.done)
	return errors.Join(restore, t.in.Close())
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
