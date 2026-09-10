// Package record defines the core's row model: the JSON value model, same
// as google.protobuf.Struct in datasplice-protocol.md, so swapping to real
// gRPC batches later (M2) doesn't change how a row looks.
package record

import "strings"

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

// Set writes a dotted path, creating intermediate maps as needed.
func (r Record) Set(path string, value any) {
	parts := strings.Split(path, ".")
	m := map[string]any(r)
	for _, part := range parts[:len(parts)-1] {
		next, ok := m[part].(map[string]any)
		if !ok {
			next = map[string]any{}
			m[part] = next
		}
		m = next
	}

	m[parts[len(parts)-1]] = value
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
