package tui

import "testing"

// bytesFrom feeds escapeKey from s, then reports no byte as a read timeout
// does; rest returns what was left unread.
func bytesFrom(s string) (next func() (byte, bool), rest func() string) {
	i := 0
	next = func() (byte, bool) {
		if i >= len(s) {
			return 0, false
		}
		i++
		return s[i-1], true
	}
	return next, func() string { return s[i:] }
}

func TestEscapeKey(t *testing.T) {
	for _, tc := range []struct {
		name, after string
		key         Key
		ok          bool
		rest        string
	}{
		{"bare escape", "", Key{Kind: KeyEscape}, true, ""},
		{"escape then [ alone", "[", Key{Kind: KeyEscape}, true, ""},
		{"up arrow", "[A", Key{Kind: KeyUp}, true, ""},
		{"down arrow", "[Bx", Key{Kind: KeyDown}, true, "x"},
		{"right arrow", "[C", Key{Kind: KeyRight}, true, ""},
		{"left arrow", "[D", Key{Kind: KeyLeft}, true, ""},
		{"ctrl-up keeps its arrow", "[1;5A", Key{Kind: KeyUp}, true, ""},
		{"delete is consumed whole and ignored", "[3~x", Key{}, false, "x"},
		{"page up is consumed whole and ignored", "[5~", Key{}, false, ""},
		{"bracketed paste start is ignored", "[200~hi", Key{}, false, "hi"},
		{"intermediate bytes are consumed", "[1 q", Key{}, false, ""},
		{"a sequence cut short is ignored", "[12", Key{}, false, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			next, rest := bytesFrom(tc.after)
			key, ok := escapeKey(next)
			if key != tc.key || ok != tc.ok {
				t.Fatalf("escapeKey(ESC %q) = %+v, %v; want %+v, %v", tc.after, key, ok, tc.key, tc.ok)
			}
			if rest() != tc.rest {
				t.Fatalf("escapeKey(ESC %q) left %q unread, want %q", tc.after, rest(), tc.rest)
			}
		})
	}
}
