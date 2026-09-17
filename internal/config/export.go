package config

import (
	"fmt"
	"maps"
	"strings"

	"github.com/datasplice-labs/datasplice-core/internal/record"
)

// Apply resolves a step's export block into the record that gets passed
// downstream. `in` is the record this step received (nil for a source step,
// which has no upstream: see internal/pipeline/export.go), `out` is the
// record this step itself produced. A nil Export passes out through unchanged.
func (e *Export) Apply(in, out record.Record) (record.Record, error) {
	if e == nil {
		return out, nil
	}

	// Build the outgoing record from scratch — we never mutate `in` or
	// `out` in place, since both may be reused elsewhere (e.g. `out` is
	// also what a source step emits directly when it has no export:).
	result := record.Record{}
	if e.Passthrough {
		// Start from a copy of everything the step produced. `values`
		// below can then add to or override individual fields on top of
		// this starting point.
		maps.Copy(result, out)
	}

	// Each entry in `values` is `new_field_name: "in.some.path"` or
	// `"out.some.path"` — resolve the scope prefix, look the rest of the
	// path up in the right record, and write it into result under its
	// new name.
	for name, path := range e.Values {
		scope, rest, ok := strings.Cut(path, ".")
		if !ok {
			return nil, fmt.Errorf("export: value %q for %q must be a dotted path starting with in. or out", path, name)
		}

		var src record.Record
		switch scope {
		case "in":
			src = in
		case "out":
			src = out
		default:
			return nil, fmt.Errorf("export: value %q for %q: unknown scope %q (want in or out)", path, name, scope)
		}

		v, ok := src.Get(rest)
		if !ok {
			return nil, fmt.Errorf("export: value %q for %q: path not found", path, name)
		}

		if err := result.Set(name, v); err != nil {
			return nil, err
		}
	}

	return result, nil
}
