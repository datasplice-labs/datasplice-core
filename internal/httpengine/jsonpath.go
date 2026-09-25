// Package httpengine executes the HTTP side of a manifest: building
// requests, auth, pagination, rate limiting, retries, and pulling records
// out of responses. It's the only place that talks HTTP on a package's
// behalf, which is why retries and rate limits are written once here.
package httpengine

import (
	"fmt"

	"github.com/theory/jsonpath"
)

// Path is a compiled JSONPath expression. It's the only thing in this
// package that knows which JSONPath library is in use.
type Path struct {
	expr string
	p    *jsonpath.Path
}

// CompilePath parses a JSONPath expression like `$.tickets[0].id`.
func CompilePath(expr string) (*Path, error) {
	p, err := jsonpath.Parse(expr)
	if err != nil {
		return nil, fmt.Errorf("jsonpath %q: %w", expr, err)
	}

	return &Path{expr: expr, p: p}, nil
}

// Select returns every value the path matches in v.
func (p *Path) Select(v any) []any {
	return []any(p.p.Select(v))
}

// One returns the value the path resolves to: the match itself when
// there's exactly one, the list of matches when there are several, and
// ok=false when there are none.
func (p *Path) One(v any) (any, bool) {
	matches := p.Select(v)
	switch len(matches) {
	case 0:
		return nil, false
	case 1:
		return matches[0], true
	default:
		return matches, true
	}
}

func (p *Path) String() string { return p.expr }
