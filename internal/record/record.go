// Package record defines the core's row model: the JSON value model, same
// as google.protobuf.Struct in datasplice-protocol.md, so swapping to real
// gRPC batches later (M2) doesn't change how a row looks.
package record

import (
	"fmt"
	"strings"
)

// Record is a single row. Values are string, float64, bool, nil,
// map[string]any, or []any — exactly what encoding/json produces.
type Record map[string]any

// Get resolves a dotted path ("requester.name") through nested maps.
func (r Record) Get(path string) (any, bool) {
	var cur any = map[string]any(r)
	for part := range strings.SplitSeq(path, ".") {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}

		cur, ok = m[part]
		if !ok {
			return nil, false
		}
	}

	return cur, true
}

// Has reports whether path resolves to any value, including nil.
func (r Record) Has(path string) bool {
	_, ok := r.Get(path)
	return ok
}

// String resolves path and type-asserts the result as a string. A missing
// path or a differently-typed value both return ("", false) rather than
// panicking.
func (r Record) String(path string) (string, bool) {
	v, ok := r.Get(path)
	if !ok {
		return "", false
	}

	s, ok := v.(string)
	return s, ok
}

// Number resolves path and type-asserts the result as a float64 — the
// numeric type encoding/json produces for every JSON number.
func (r Record) Number(path string) (float64, bool) {
	v, ok := r.Get(path)
	if !ok {
		return 0, false
	}

	n, ok := v.(float64)
	return n, ok
}

// Bool resolves path and type-asserts the result as a bool.
func (r Record) Bool(path string) (bool, bool) {
	v, ok := r.Get(path)
	if !ok {
		return false, false
	}

	b, ok := v.(bool)
	return b, ok
}

// Keys returns the record's top-level keys, in no particular order.
func (r Record) Keys() []string {
	keys := make([]string, 0, len(r))
	for k := range r {
		keys = append(keys, k)
	}

	return keys
}

// Clone returns a deep copy: nested maps and slices are copied too, so
// mutating the clone never affects the original.
func (r Record) Clone() Record {
	return Record(cloneValue(map[string]any(r)).(map[string]any))
}

func cloneValue(v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, vv := range t {
			out[k] = cloneValue(vv)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, vv := range t {
			out[i] = cloneValue(vv)
		}
		return out
	default:
		return v
	}
}

// Set writes a dotted path, creating intermediate maps as needed. It
// errors if an intermediate segment already holds a non-map value —
// overwriting it silently would lose whatever was there.
func (r Record) Set(path string, value any) error {
	parts := strings.Split(path, ".")
	m := map[string]any(r)
	for i, part := range parts[:len(parts)-1] {
		existing, ok := m[part]
		if !ok {
			next := map[string]any{}
			m[part] = next
			m = next
			continue
		}
		next, ok := existing.(map[string]any)
		if !ok {
			return fmt.Errorf("record: cannot set %q: %q is a %T, not an object", path, strings.Join(parts[:i+1], "."), existing)
		}
		m = next
	}

	m[parts[len(parts)-1]] = value
	return nil
}

// Delete removes a dotted path. No-op if it doesn't exist.
func (r Record) Delete(path string) {
	parts := strings.Split(path, ".")
	m := map[string]any(r)
	for _, part := range parts[:len(parts)-1] {
		next, ok := m[part].(map[string]any)
		if !ok {
			return
		}
		m = next
	}
	delete(m, parts[len(parts)-1])
}
