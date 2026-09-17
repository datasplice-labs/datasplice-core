// Package contract is the in-process package contract: every
// builtin implements it directly, no gRPC, no subprocess. The swap to a
// real go-plugin client is a transport change, not a redesign.
package contract

import (
	"context"
	"slices"
	"strings"

	"github.com/datasplice-labs/datasplice-core/internal/record"
)

// Role mirrors datasplice.v1.Role — a package declares the set it can act
// as (Describe.Roles), statically, so `plan` can check pipeline
// composition offline. pipeline.Build picks whichever declared role fits
// a step's position (first -> source, last -> sink, otherwise ->transform);
// most builtins declare exactly one and have no choice to make.
// A package that declares more than one (e.g. `json`, which can read or write)
// tells which one it's playing at runtime the same way
// Process always has:
//
//	in == nil means "I'm the source here"
//	out == nil means "I'm the sink here"
//
// The position decides the role, not a field.
type Role int

const (
	RoleUnspecified Role = iota
	RoleSource           // emits only
	RoleTransform        // consumes and emits
	RoleSink             // consumes only
)

func (r Role) String() string {
	switch r {
	case RoleSource:
		return "source"
	case RoleTransform:
		return "transform"
	case RoleSink:
		return "sink"
	default:
		return "unspecified"
	}
}

// SettingSpec mirrors datasplice.v1.SettingSpec. Optional; a package that
// declares these gets its `with:` block schema-checked before it runs.
type SettingSpec struct {
	Key      string
	Type     string
	Required bool
	Secret   bool
}

// Describe mirrors datasplice.v1.DescribeResponse. Must be answerable
// without Configure ever having been called.
type Describe struct {
	Name     string
	Version  string
	Roles    []Role
	Settings []SettingSpec
}

// HasRole reports whether the package can act as r.
func (d Describe) HasRole(r Role) bool {
	return slices.Contains(d.Roles, r)
}

// RolesString formats Roles for error messages, e.g. "source/sink".
func (d Describe) RolesString() string {
	if len(d.Roles) == 0 {
		return RoleUnspecified.String()
	}

	parts := make([]string, len(d.Roles))
	for i, r := range d.Roles {
		parts[i] = r.String()
	}

	return strings.Join(parts, "/")
}

// Batch mirrors datasplice.v1.RecordBatch.
type Batch []record.Record

// Package is what every builtin implements. A source ignores in (nil) and
// closes out when done; a sink drains in and never writes to out (nil); a
// transform does both. Process must respect ctx cancellation in every
// select, including sends on out — a step that blocks on a send after its
// downstream has stopped reading would deadlock the whole pipeline.
type Package interface {
	Describe() Describe

	// Configure is called exactly once, before Process. settings is the
	// step's `with:` block after interpolation; secrets holds only the
	// values this step referenced (datasplice-protocol.md §3).
	Configure(settings map[string]any, secrets map[string]string) error

	Process(ctx context.Context, in <-chan Batch, out chan<- Batch) error
}
