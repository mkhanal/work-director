package crash

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"
)

// Report is what a macOS crash report (.ips) says about how the process died.
type Report struct {
	// Signal is the exception the kernel raised, as "EXC_BREAKPOINT (SIGTRAP)".
	Signal string `json:"signal"`
	// Messages are what the process and its libraries said as they died, a
	// Swift runtime error among them.
	Messages []string `json:"messages"`
	// Thrown is the stack an Objective-C exception was thrown from, as the
	// report prints it; empty when none was.
	Thrown []string `json:"thrown"`
	// Thread is the crashing thread, innermost frame first.
	Thread []Frame `json:"thread"`
}

// Frame is one frame of the crashing thread.
type Frame struct {
	Image  string `json:"image"`
	Symbol string `json:"symbol"`
	Offset int    `json:"offset"`
	File   string `json:"file"`
	Line   int    `json:"line"`
}

func (f Frame) String() string {
	s := f.Image + "  "
	if f.Symbol == "" {
		s += fmt.Sprintf("0x%x", f.Offset)
	} else {
		s += fmt.Sprintf("%s + %d", f.Symbol, f.Offset)
	}
	if f.File != "" {
		s += fmt.Sprintf("  (%s:%d)", f.File, f.Line)
	}
	return s
}

// ips is the part of a report's body read here.
type ips struct {
	Exception struct {
		Type   string `json:"type"`
		Signal string `json:"signal"`
	} `json:"exception"`
	ASI            map[string][]string `json:"asi"`
	ASIBacktraces  []string            `json:"asiBacktraces"`
	FaultingThread int                 `json:"faultingThread"`
	Threads        []struct {
		Frames []struct {
			ImageIndex     int    `json:"imageIndex"`
			ImageOffset    int    `json:"imageOffset"`
			Symbol         string `json:"symbol"`
			SymbolLocation int    `json:"symbolLocation"`
			SourceFile     string `json:"sourceFile"`
			SourceLine     int    `json:"sourceLine"`
		} `json:"frames"`
	} `json:"threads"`
	UsedImages []struct {
		Name string `json:"name"`
	} `json:"usedImages"`
}

// ParseReport reads an .ips file: one line of JSON header, then the body as
// one JSON document.
func ParseReport(data []byte) (Report, error) {
	_, body, ok := strings.Cut(string(data), "\n")
	if !ok {
		return Report{}, fmt.Errorf("not a crash report: no body after the header line")
	}
	var r ips
	if err := json.Unmarshal([]byte(body), &r); err != nil {
		return Report{}, fmt.Errorf("not a crash report: %w", err)
	}
	if r.FaultingThread < 0 || r.FaultingThread >= len(r.Threads) {
		return Report{}, fmt.Errorf("not a crash report: faulting thread %d of %d", r.FaultingThread, len(r.Threads))
	}
	out := Report{Signal: fmt.Sprintf("%s (%s)", r.Exception.Type, r.Exception.Signal), Messages: []string{}, Thrown: []string{}, Thread: []Frame{}}
	for _, lib := range slices.Sorted(maps.Keys(r.ASI)) {
		for _, l := range r.ASI[lib] {
			out.Messages = append(out.Messages, strings.TrimSpace(l))
		}
	}
	for _, b := range r.ASIBacktraces {
		for _, l := range strings.Split(strings.TrimRight(b, "\n"), "\n") {
			out.Thrown = append(out.Thrown, l)
		}
	}
	for _, f := range r.Threads[r.FaultingThread].Frames {
		frame := Frame{Symbol: f.Symbol, File: f.SourceFile, Line: f.SourceLine, Offset: f.ImageOffset}
		if f.Symbol != "" {
			frame.Offset = f.SymbolLocation
		}
		if f.ImageIndex >= 0 && f.ImageIndex < len(r.UsedImages) {
			frame.Image = r.UsedImages[f.ImageIndex].Name
		}
		out.Thread = append(out.Thread, frame)
	}
	return out, nil
}
