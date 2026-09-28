package taste

import (
	"regexp"
	"strings"
)

type Frontmatter struct {
	Fields map[string]string
	Body   string
}

var frontmatterRe = regexp.MustCompile(`(?s)^---\n(.*?)\n---\n?(.*)$`)

// ParseFrontmatter splits a card's YAML-ish frontmatter from its body. The
// frontmatter is line-oriented key: value pairs; the body is everything after
// the closing ---, trimmed. It returns nil when the text has no frontmatter
// block or a line without a key.
func ParseFrontmatter(text string) *Frontmatter {
	m := frontmatterRe.FindStringSubmatch(text)
	if m == nil {
		return nil
	}
	fields := make(map[string]string)
	for _, line := range strings.Split(m[1], "\n") {
		i := strings.IndexByte(line, ':')
		if i < 1 {
			return nil
		}
		fields[strings.TrimSpace(line[:i])] = strings.TrimSpace(line[i+1:])
	}
	return &Frontmatter{Fields: fields, Body: strings.TrimSpace(m[2])}
}

// List parses a bracketed, comma-separated frontmatter list. An empty or
// missing list yields an empty slice.
func List(raw string) []string {
	inner := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(raw, "["), "]"))
	if inner == "" {
		return []string{}
	}
	parts := strings.Split(inner, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		out = append(out, strings.TrimSpace(p))
	}
	return out
}
