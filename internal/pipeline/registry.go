package pipeline

import (
	"fmt"
	"strings"

	"github.com/datasplice-labs/datasplice-core/internal/builtin"
	"github.com/datasplice-labs/datasplice-core/internal/contract"
)

// registry maps first-party module names to a builtin factory. Third-party
// resolution (go-install, lockfile, cache) is M3 (datasplice-core-prd.md
// §4/§8) and isn't implemented yet.
var registry = map[string]func() contract.Package{
	"datasplice/csv":  func() contract.Package { return builtin.NewCSV() },
	"datasplice/json": func() contract.Package { return builtin.NewJSON() },
	"datasplice/map":  func() contract.Package { return builtin.NewMap() },
	"datasplice/http": func() contract.Package { return builtin.NewHTTP() },
}

// resolve turns a `uses:` ref into a fresh package instance. Only
// first-party @latest refs work in M0/M1; @latest is otherwise rejected
// for third-party refs per datasplice-core-prd.md §4, but since no
// resolver exists yet, every third-party ref fails regardless of version.
func resolve(uses string) (contract.Package, error) {
	module, version, ok := strings.Cut(uses, "@")
	if !ok {
		return nil, fmt.Errorf("%q: expected <module>@<version>", uses)
	}
	factory, ok := registry[module]
	if !ok {
		return nil, fmt.Errorf("%s: not a first-party package and third-party resolution isn't implemented yet (lands in M3)", module)
	}
	if version != "latest" {
		return nil, fmt.Errorf("%s: first-party packages only support @latest, got %q", module, version)
	}
	return factory(), nil
}
