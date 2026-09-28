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

// resolve turns a `uses:` ref into a fresh package instance. Built-ins are
// pinned to the core's own version, so a version suffix is optional for
// them and only `@latest` is accepted when given (docs/getting-started/
// file-structure.md "datasplice.lock": "@latest is allowed for built-in
// packages ... It's rejected for third-party ones").
func resolve(uses string) (contract.Package, error) {
	module, version, hasVersion := strings.Cut(uses, "@")

	factory, ok := registry[module]
	if !ok {
		// resolveStep already routes local refs (./x) and third-party
		// refs (host.tld/path@version) elsewhere before falling through
		// to here — reaching this with neither shape means module isn't
		// a real first-party name.
		return nil, fmt.Errorf("%s: not a first-party package (want datasplice/<name>, a local ./path, or <host>/<path>@<version>)", module)
	}

	if hasVersion && version != "latest" {
		return nil, fmt.Errorf("%s: built-in packages are pinned to the core's own version; only @latest or no version is accepted, got %q", module, version)
	}

	return factory(), nil
}
