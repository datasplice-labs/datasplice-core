package pipeline

import (
	"fmt"
	"strings"

	"github.com/datasplice-labs/datasplice-core/internal/builtin"
	"github.com/datasplice-labs/datasplice-core/internal/contract"
)

// registry maps first-party module names to a builtin factory.
// Those are just the first-party packages.
var registry = map[string]func() contract.Package{
	"datasplice/csv":  func() contract.Package { return builtin.NewCSV() },
	"datasplice/json": func() contract.Package { return builtin.NewJSON() },
	"datasplice/map":  func() contract.Package { return builtin.NewMap() },
	"datasplice/http": func() contract.Package { return builtin.NewHTTP() },
}

// resolve turns a `uses:` ref into a fresh package instance.
func resolve(uses string) (contract.Package, error) {
	module, _, ok := strings.Cut(uses, "@")
	if !ok {
		return nil, fmt.Errorf("%q: expected <module>@<version>", uses)
	}

	factory, ok := registry[module]
	if !ok {
		return nil, fmt.Errorf("%s: not a first-party package. Third-party resolution isn't available yet", module)
	}

	return factory(), nil
}
