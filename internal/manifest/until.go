package manifest

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// UntilKind is which stop condition a paginate.until expression is.
type UntilKind int

const (
	UntilNone    UntilKind = iota // no until: pagination ends when there's nothing to follow
	UntilEmpty                    // stop after a page with no records
	UntilCompare                  // stop when a response value equals (or differs from) a literal
)

// Until is a parsed paginate.until. Only two forms exist: `empty`, and a
// single comparison like `$.meta.has_more == false`.
type Until struct {
	Kind  UntilKind
	Path  string // UntilCompare: JSONPath into the response
	Equal bool   // UntilCompare: true for ==, false for !=
	Value any    // UntilCompare: bool, float64, string or nil
}

// The path can't contain spaces, so the operator is the first `==`/`!=`
// with whitespace around it.
var compareRe = regexp.MustCompile(`^(\$\S*)\s+(==|!=)\s+(.+)$`)

// ParseUntil parses a paginate.until expression. Anything more complex
// than the two supported forms is an error.
func ParseUntil(s string) (Until, error) {
	s = strings.TrimSpace(s)
	switch s {
	case "":
		return Until{Kind: UntilNone}, nil
	case "empty":
		return Until{Kind: UntilEmpty}, nil
	}

	m := compareRe.FindStringSubmatch(s)
	if m == nil {
		return Until{}, fmt.Errorf("until %q: want `empty` or a single comparison like `$.meta.has_more == false`", s)
	}

	v, err := parseLiteral(m[3])
	if err != nil {
		return Until{}, fmt.Errorf("until %q: %w", s, err)
	}

	return Until{Kind: UntilCompare, Path: m[1], Equal: m[2] == "==", Value: v}, nil
}

func parseLiteral(s string) (any, error) {
	switch s {
	case "true":
		return true, nil
	case "false":
		return false, nil
	case "null":
		return nil, nil
	}

	if n, err := strconv.ParseFloat(s, 64); err == nil {
		return n, nil
	}

	if len(s) >= 2 && (s[0] == '"' || s[0] == '\'') && s[len(s)-1] == s[0] {
		return s[1 : len(s)-1], nil
	}

	return nil, fmt.Errorf("value %q must be true, false, null, a number, or a quoted string", s)
}
