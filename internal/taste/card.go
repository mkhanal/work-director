// Package taste holds the taste build: rule cards under taste/cards are the
// source, and the build renders them into the plugin skills and the dist
// artifacts. Nothing under plugin/ or dist/ is hand-edited; `go run
// ./cmd/taste` regenerates all of it.
package taste

import (
	"fmt"
	"regexp"
	"strings"
)

type Category string

const (
	CategoryJudgment        Category = "judgment"
	CategoryAlternatives    Category = "alternatives"
	CategoryOrganisation    Category = "organisation"
	CategoryTypesAndSchemas Category = "types-and-schemas"
	CategoryDefensiveCoding Category = "defensive-coding"
	CategoryComments        Category = "comments"
	CategoryWorkingMethod   Category = "working-method"
	CategoryCommunication   Category = "communication"
)

var Categories = []Category{
	CategoryJudgment,
	CategoryAlternatives,
	CategoryOrganisation,
	CategoryTypesAndSchemas,
	CategoryDefensiveCoding,
	CategoryComments,
	CategoryWorkingMethod,
	CategoryCommunication,
}

type Kind string

const (
	KindPrinciple  Kind = "principle"
	KindPractice   Kind = "practice"
	KindMechanical Kind = "mechanical"
)

var Kinds = []Kind{KindPrinciple, KindPractice, KindMechanical}

type Status string

const (
	StatusCandidate Status = "candidate"
	StatusAdopted   Status = "adopted"
	StatusRetired   Status = "retired"
)

var Statuses = []Status{StatusCandidate, StatusAdopted, StatusRetired}

// Scope is where a card applies: global, or a lang/stack/project/team tag.
type Scope string

type Card struct {
	ID        string   `json:"id"`
	Title     string   `json:"title"`
	Category  Category `json:"category"`
	Scope     []Scope  `json:"scope"`
	Kind      Kind     `json:"kind"`
	Status    Status   `json:"status"`
	Always    bool     `json:"always"`
	Enforce   []string `json:"enforce"`
	Evidence  []string `json:"evidence"`
	Statement string   `json:"statement"`
	Body      string   `json:"body"`
	Path      string   `json:"path"`
}

type CardError struct {
	Path   string
	Detail string
}

func (e *CardError) Error() string { return fmt.Sprintf("%s: %s", e.Path, e.Detail) }

var scopeRe = regexp.MustCompile(`^(global|(lang|stack|project|team):[a-z0-9][a-z0-9-]*)$`)

func isOneOf[T comparable](values []T, v T) bool {
	for _, s := range values {
		if s == v {
			return true
		}
	}
	return false
}

// ParseCard parses one rule card: frontmatter fields plus the body, whose
// first non-empty line is the statement. It rejects unknown categories,
// kinds, statuses, malformed scopes and missing required fields.
func ParseCard(text, path string) (*Card, error) {
	fm := ParseFrontmatter(text)
	if fm == nil {
		return nil, &CardError{path, "missing frontmatter"}
	}
	need := func(k string) (string, error) {
		v, ok := fm.Fields[k]
		if !ok || v == "" {
			return "", &CardError{path, "missing field " + k}
		}
		return v, nil
	}
	id, err := need("id")
	if err != nil {
		return nil, err
	}
	title, err := need("title")
	if err != nil {
		return nil, err
	}
	category, err := need("category")
	if err != nil {
		return nil, err
	}
	if !isOneOf(Categories, Category(category)) {
		return nil, &CardError{path, "unknown category " + category}
	}
	kind, err := need("kind")
	if err != nil {
		return nil, err
	}
	if !isOneOf(Kinds, Kind(kind)) {
		return nil, &CardError{path, "unknown kind " + kind}
	}
	status, err := need("status")
	if err != nil {
		return nil, err
	}
	if !isOneOf(Statuses, Status(status)) {
		return nil, &CardError{path, "unknown status " + status}
	}
	always, err := need("always")
	if err != nil {
		return nil, err
	}
	if always != "true" && always != "false" {
		return nil, &CardError{path, "always must be true or false"}
	}
	scopeRaw, err := need("scope")
	if err != nil {
		return nil, err
	}
	scope := List(scopeRaw)
	if len(scope) == 0 {
		return nil, &CardError{path, "bad scope []"}
	}
	scopes := make([]Scope, 0, len(scope))
	for _, s := range scope {
		if !scopeRe.MatchString(s) {
			return nil, &CardError{path, "bad scope " + s}
		}
		scopes = append(scopes, Scope(s))
	}
	statement := ""
	for _, l := range strings.Split(fm.Body, "\n") {
		if strings.TrimSpace(l) != "" {
			statement = l
			break
		}
	}
	if statement == "" {
		return nil, &CardError{path, "empty body"}
	}
	return &Card{
		ID:        id,
		Title:     title,
		Category:  Category(category),
		Scope:     scopes,
		Kind:      Kind(kind),
		Status:    Status(status),
		Always:    always == "true",
		Enforce:   List(fm.Fields["enforce"]),
		Evidence:  List(fm.Fields["evidence"]),
		Statement: statement,
		Body:      fm.Body,
		Path:      path,
	}, nil
}
