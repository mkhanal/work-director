package runner

import (
	"os"
	"path/filepath"
	"testing"
)

func TestATranscriptReadsWhatWasAddedAndStartsOverWhenRewritten(t *testing.T) {
	file := filepath.Join(t.TempDir(), "s.jsonl")
	line := func(text string) string {
		return `{"type":"assistant","timestamp":"2026-10-11T10:00:00Z","message":{"content":[{"type":"text","text":"` + text + `"}]}}` + "\n"
	}
	write := func(s string) {
		t.Helper()
		if err := os.WriteFile(file, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	read := func() []string {
		t.Helper()
		got, err := conversationIn(file, "", false)
		if err != nil {
			t.Fatal(err)
		}
		var texts []string
		for _, e := range got {
			texts = append(texts, e.Text)
		}
		return texts
	}
	write(line("one"))
	if got := read(); len(got) != 1 || got[0] != "one" {
		t.Fatalf("first read = %q", got)
	}
	write(line("one") + line("two") + `{"type":"assistant","message":{"content":[{"type":"te`)
	if got := read(); len(got) != 2 || got[1] != "two" {
		t.Fatalf("grown = %q, want the added line and not the one still being written", got)
	}
	write(line("uno") + line("dos") + line("tres"))
	if got := read(); len(got) != 3 || got[0] != "uno" {
		t.Fatalf("rewritten = %q, want it read again from the start", got)
	}
}

func TestOnlyTheMostRecentlyReadTranscriptsStayParsed(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < keptTranscripts+5; i++ {
		file := filepath.Join(dir, string(rune('a'+i%26))+string(rune('a'+i/26))+".jsonl")
		if err := os.WriteFile(file, []byte(`{"type":"user","message":{"content":"hi"}}`+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := conversationIn(file, "", false); err != nil {
			t.Fatal(err)
		}
	}
	transcripts.Lock()
	defer transcripts.Unlock()
	if len(transcripts.m) > keptTranscripts {
		t.Fatalf("%d transcripts kept, want at most %d", len(transcripts.m), keptTranscripts)
	}
}
