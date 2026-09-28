package runner

import (
	"fmt"
	"regexp"
	"strings"
)

var tomlKeyRe = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// parseSpecTOML parses the runner-spec subset of TOML: flat `key = value`
// pairs where a value is a basic string ("..." with escapes), a literal string
// ('...' with no escapes) or a boolean, plus `#` comments and blank lines.
// Spec files are flat string maps by design; this is not a general TOML
// parser and says so instead of guessing at tables or arrays.
func parseSpecTOML(text string) (RunnerSpec, error) {
	var spec RunnerSpec
	for i, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		eq := strings.Index(line, "=")
		if eq < 0 {
			return RunnerSpec{}, fmt.Errorf("line %d: expected key = value", i+1)
		}
		key := strings.TrimSpace(line[:eq])
		if !tomlKeyRe.MatchString(key) {
			return RunnerSpec{}, fmt.Errorf("line %d: invalid key %q", i+1, key)
		}
		raw := strings.TrimSpace(line[eq+1:])
		value, err := parseSpecValue(raw)
		if err != nil {
			return RunnerSpec{}, fmt.Errorf("line %d: %v", i+1, err)
		}
		if err := spec.set(key, value); err != nil {
			return RunnerSpec{}, fmt.Errorf("line %d: %v", i+1, err)
		}
	}
	return spec, nil
}

// parseSpecValue parses one spec value: a basic string, a literal string, or
// a boolean. The bool result is false for strings.
func parseSpecValue(raw string) (string, error) {
	switch {
	case raw == "true":
		return "true", nil
	case raw == "false":
		return "false", nil
	case strings.HasPrefix(raw, `"`):
		return parseBasicString(raw)
	case strings.HasPrefix(raw, `'`):
		return parseLiteralString(raw)
	default:
		return "", fmt.Errorf("unrecognized value %q", raw)
	}
}

func (s *RunnerSpec) set(key, value string) error {
	switch key {
	case "name":
		s.Name = value
	case "spawn":
		s.Spawn = value
	case "session_id":
		s.SessionID = value
	case "send":
		s.Send = value
	case "status":
		s.Status = value
	case "running":
		s.Running = value
	case "waiting":
		s.Waiting = value
	case "exited":
		s.Exited = value
	case "transcript":
		s.Transcript = value
	case "models":
		s.Models = value
	case "attach":
		s.Attach = value
	case "detach":
		if value == "true" {
			s.Detach = true
			return nil
		}
		if value == "false" {
			return nil
		}
		return fmt.Errorf("detach must be true or false")
	default:
		return fmt.Errorf("unknown key %q", key)
	}
	return nil
}

// parseBasicString parses a TOML basic string: double-quoted, with escapes.
func parseBasicString(s string) (string, error) {
	var b strings.Builder
	for i := 1; i < len(s); i++ {
		c := s[i]
		if c == '"' {
			return b.String(), nil
		}
		if c != '\\' {
			b.WriteByte(c)
			continue
		}
		i++
		if i >= len(s) {
			return "", fmt.Errorf("unterminated escape")
		}
		switch s[i] {
		case 'n':
			b.WriteByte('\n')
		case 't':
			b.WriteByte('\t')
		case 'r':
			b.WriteByte('\r')
		case '"':
			b.WriteByte('"')
		case '\\':
			b.WriteByte('\\')
		default:
			return "", fmt.Errorf("unknown escape \\%c", s[i])
		}
	}
	return "", fmt.Errorf("unterminated string")
}

// parseLiteralString parses a TOML literal string: single-quoted, no escapes.
func parseLiteralString(s string) (string, error) {
	end := strings.Index(s[1:], "'")
	if end < 0 {
		return "", fmt.Errorf("unterminated string")
	}
	return s[1 : 1+end], nil
}
