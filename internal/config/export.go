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

	result := record.Record{}
	if e.Passthrough {
		// If the export is Passthrough, we copy the `out` record into the new record.
		maps.Copy(result, out)
	}

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
