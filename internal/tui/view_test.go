package tui

import (
	"strings"
	"testing"
)

func TestDetailFrameWithAStatusFitsTheTerminal(t *testing.T) {
	lines := DetailFrame(longDetail(), Model{View: ViewDetail, Width: 80, Height: 24, Status: "sent"})
	text := strings.Join(lines, "\n")
	if len(lines) > 24 {
		t.Fatalf("frame is %d lines, taller than the terminal's 24:\n%s", len(lines), text)
	}
	assertContains(t, text, "\n\nsent\n")
}
