package runner

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Usage is what a session has spent and done: the model it ran on, the
// tokens it was sent new and wrote, the tools it called, and when it began
// and last wrote.
type Usage struct {
	Model   string  `json:"model,omitempty"`
	Tokens  int     `json:"tokens"`
	Tools   int     `json:"tools"`
	Started *string `json:"started,omitempty"`
	Last    *string `json:"last,omitempty"`
}

// Subagent is an agent a session started for part of its work.
type Subagent struct {
	ID          string `json:"id"`
	Description string `json:"description"`
	Type        string `json:"type"`
	// Done is whether its last reply ended its turn.
	Done bool `json:"done"`
	// Said is the first line of the last thing it said.
	Said  string `json:"said,omitempty"`
	Usage Usage  `json:"usage"`
}

// Inspector is a runner that can say what a session spent and which
// subagents it started, and show each subagent's own conversation.
type Inspector interface {
	Usage(h Handle) (Usage, error)
	Subagents(h Handle) ([]Subagent, error)
	SubagentConversation(h Handle, agent string) ([]Entry, error)
}

// spent reads a store's usage, whether its last reply ended its turn, and
// the first line of the last thing said.
func spent(file, cwd string, sidechain bool) (Usage, bool, string, error) {
	t, err := readTranscript(file, cwd, sidechain)
	if err != nil {
		return Usage{}, false, "", err
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.usage, t.ended, t.said, nil
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	return strings.TrimSpace(line)
}

func (claudeRunner) Usage(h Handle) (Usage, error) {
	file, err := transcriptFile(h.Session)
	if err != nil || file == "" {
		return Usage{}, err
	}
	u, _, _, err := spent(file, h.Cwd, false)
	return u, err
}

// subagentDir is where claude keeps the stores of a session's subagents:
// beside the session's own, in a directory named for it.
func subagentDir(session string) (string, error) {
	file, err := transcriptFile(session)
	if err != nil || file == "" {
		return "", err
	}
	return filepath.Join(strings.TrimSuffix(file, ".jsonl"), "subagents"), nil
}

func (claudeRunner) Subagents(h Handle) ([]Subagent, error) {
	out := []Subagent{}
	dir, err := subagentDir(h.Session)
	if err != nil || dir == "" {
		return out, err
	}
	stores, err := filepath.Glob(filepath.Join(dir, "agent-*.jsonl"))
	if err != nil {
		return nil, err
	}
	for _, store := range stores {
		id := strings.TrimSuffix(strings.TrimPrefix(filepath.Base(store), "agent-"), ".jsonl")
		a := Subagent{ID: id}
		meta, err := os.ReadFile(strings.TrimSuffix(store, ".jsonl") + ".meta.json")
		switch {
		case err == nil:
			var m struct {
				Description string `json:"description"`
				AgentType   string `json:"agentType"`
			}
			if err := json.Unmarshal(meta, &m); err != nil {
				return nil, fmt.Errorf("%s: %w", store, err)
			}
			a.Description, a.Type = m.Description, m.AgentType
		case !errors.Is(err, fs.ErrNotExist):
			return nil, err
		}
		if a.Usage, a.Done, a.Said, err = spent(store, h.Cwd, true); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool {
		return stamp(out[i].Usage.Started) < stamp(out[j].Usage.Started)
	})
	return out, nil
}

func stamp(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func (claudeRunner) SubagentConversation(h Handle, agent string) ([]Entry, error) {
	dir, err := subagentDir(h.Session)
	if err != nil {
		return nil, err
	}
	store := filepath.Join(dir, "agent-"+agent+".jsonl")
	if dir == "" || strings.ContainsAny(agent, `/\`) {
		return nil, &RunnerError{Runner: "claude", Detail: "session " + h.Session + " has no subagent " + agent}
	}
	if _, err := os.Stat(store); err != nil {
		return nil, &RunnerError{Runner: "claude", Detail: "session " + h.Session + " has no subagent " + agent}
	}
	return conversationIn(store, h.Cwd, true)
}
