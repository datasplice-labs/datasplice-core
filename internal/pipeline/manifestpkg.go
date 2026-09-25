package pipeline

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/datasplice-labs/datasplice-core/internal/config"
	"github.com/datasplice-labs/datasplice-core/internal/contract"
	"github.com/datasplice-labs/datasplice-core/internal/httpengine"
	"github.com/datasplice-labs/datasplice-core/internal/manifest"
)

// isLocalRef reports whether uses points at a manifest file on disk
// (./datasplice.yaml, ../x/datasplice.yaml, an absolute path) instead of
// a published package. It lets a package author run a manifest before
// tagging it.
func isLocalRef(uses string) bool {
	for _, prefix := range []string{"./", "../", `.\`, `..\`} {
		if strings.HasPrefix(uses, prefix) {
			return true
		}
	}

	return filepath.IsAbs(uses)
}

// resolveStep turns a step into a package: a manifest on disk, or a
// builtin. Fields that only make sense for manifest packages are
// rejected on builtins rather than silently ignored.
func resolveStep(s config.Step) (contract.Package, error) {
	if isLocalRef(s.Uses) {
		m, err := manifest.Load(s.Uses)
		if err != nil {
			return nil, err
		}

		return httpengine.NewSourcePackage(m, s.Action, s.MaxRecords)
	}

	p, err := resolve(s.Uses)
	if err != nil {
		return nil, err
	}

	if s.Action != "" {
		return nil, fmt.Errorf("`action` is only valid on manifest packages, not %s", s.Uses)
	}

	if s.MaxRecords != 0 {
		setter, ok := p.(maxRecordsSetter)
		if !ok {
			return nil, fmt.Errorf("`max_records` is not supported on %s", s.Uses)
		}

		setter.SetMaxRecords(s.MaxRecords)
	}

	return p, nil
}

// stepValidator is implemented by packages that can check a step's
// `with:` and `secrets:` offline, before anything runs.
type stepValidator interface {
	Validate(with map[string]any, granted []string) error
}

// maxRecordsSetter is implemented by builtins backed by httpengine (only
// datasplice/http today) that want the step-level `max_records:` a
// manifest-file step already gets via httpengine.NewSourcePackage.
type maxRecordsSetter interface {
	SetMaxRecords(int)
}
