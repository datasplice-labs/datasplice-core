// Package registry resolves third-party `uses:` refs
// (github.com/org/repo@v0.2.0) to a cached datasplice.yaml, verified
// against datasplice.lock — fetch, cache, and lockfile, the piece
// internal/pipeline/registry.go's builtin table doesn't cover. Nothing
// here ever executes fetched content; a manifest is data, not code.
package registry

import (
	"fmt"
	"strings"
)

// Ref is a parsed third-party reference: <host>/<path>@<version>.
type Ref struct {
	Host    string // e.g. "github.com"
	Path    string // e.g. "myorg/datasplice-zendesk"
	Version string // e.g. "v0.2.0", or "latest"
}

// String is the ref in `uses:` form, e.g. "github.com/org/repo@v0.2.0" —
// also the exact key used in datasplice.lock.
func (r Ref) String() string { return r.Host + "/" + r.Path + "@" + r.Version }

// IsThirdParty reports whether uses looks like a third-party ref at all
// (host/path@version, as opposed to a first-party "datasplice/x" or a
// local manifest path) — checked before ParseRef, which assumes it does.
func IsThirdParty(uses string) bool {
	module, _, ok := strings.Cut(uses, "@")
	if !ok || strings.HasPrefix(module, "datasplice/") {
		return false
	}

	host, _, ok := strings.Cut(module, "/")
	return ok && strings.Contains(host, ".")
}

// ParseRef parses uses into a Ref. Call IsThirdParty first — ParseRef
// assumes the shape already looks right and just fills in the pieces.
func ParseRef(uses string) (Ref, error) {
	module, version, ok := strings.Cut(uses, "@")
	if !ok || version == "" {
		return Ref{}, fmt.Errorf("%q: expected <host>/<path>@<version>", uses)
	}

	host, path, ok := strings.Cut(module, "/")
	if !ok || host == "" || path == "" {
		return Ref{}, fmt.Errorf("%q: expected <host>/<path>@<version>", uses)
	}

	return Ref{Host: host, Path: path, Version: version}, nil
}
