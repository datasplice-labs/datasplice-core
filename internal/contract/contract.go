// Package contract is the in-process package contract: every
// builtin implements it directly, no gRPC, no subprocess. The swap to a
// real go-plugin client is a transport change, not a redesign.
package contract

import (
	"context"

	"github.com/datasplice-labs/datasplice-core/internal/record"
)

// Role mirrors datasplice.v1.Role — a package declares exactly one,
// statically, so `plan` can check pipeline composition offline.
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

// Kind mirrors datasplice.v1.Kind — only meaningful on a Function.
type Kind int

const (
	KindUnspecified Kind = iota
	KindMap              // 1 -> 1
	KindFilter           // 1 -> 0 or 1
	KindReduce           // N -> 1; buffers, emits after input closes
)

// Function mirrors datasplice.v1.Function — a named `fn:` a transform
// step can target.
type Function struct {
	Name    string
	Kind    Kind
	Accepts []string
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
	Name      string
	Version   string
	Role      Role
	Functions []Function
	Settings  []SettingSpec
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
	Configure(settings map[string]any, fn string, on []string, secrets map[string]string) error

	Process(ctx context.Context, in <-chan Batch, out chan<- Batch) error
}
