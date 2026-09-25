package httpengine

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/datasplice-labs/datasplice-core/internal/contract"
	"github.com/datasplice-labs/datasplice-core/internal/manifest"
	"github.com/datasplice-labs/datasplice-core/internal/record"
)

// SourcePackage runs one source action of a manifest as a pipeline step.
type SourcePackage struct {
	m          *manifest.Manifest
	action     string
	maxRecords int
	engine     *Engine
}

// NewSourcePackage checks the action exists and is a source. Nothing is
// resolved yet: that happens in Configure, once settings and secrets are
// known.
func NewSourcePackage(m *manifest.Manifest, action string, maxRecords int) (*SourcePackage, error) {
	a, ok := m.Actions[action]
	if !ok {
		names := make([]string, 0, len(m.Actions))
		for n := range m.Actions {
			names = append(names, n)
		}

		sort.Strings(names)

		if action == "" {
			return nil, fmt.Errorf("`action` is required for package %s (available: %s)", m.Name, strings.Join(names, ", "))
		}

		return nil, fmt.Errorf("package %s has no action %q (available: %s)", m.Name, action, strings.Join(names, ", "))
	}

	if a.Role != "source" {
		return nil, fmt.Errorf("action %q is a %s: only source actions are allowed", action, a.Role)
	}

	return &SourcePackage{m: m, action: action, maxRecords: maxRecords}, nil
}

func (p *SourcePackage) Describe() contract.Describe {
	specs := make([]contract.SettingSpec, 0, len(p.m.Settings))
	for key, s := range p.m.Settings {
		specs = append(specs, contract.SettingSpec{Key: key, Type: s.Type, Required: s.Required})
	}

	slices.SortFunc(specs, func(a, b contract.SettingSpec) int { return strings.Compare(a.Key, b.Key) })

	return contract.Describe{
		Name:     p.m.Name,
		Version:  p.m.Version,
		Roles:    []contract.Role{contract.RoleSource},
		Settings: specs,
	}
}

// Validate is the offline check pipeline.Build runs: the step's `with:`
// against the manifest's settings, and its `secrets:` against what the
// manifest declares it needs.
func (p *SourcePackage) Validate(with map[string]any, granted []string) error {
	if err := p.m.ValidateWith(with); err != nil {
		return err
	}

	return p.m.ValidateSecretsCovered(granted)
}

func (p *SourcePackage) Configure(settings map[string]any, secrets map[string]string) error {
	e, err := New(p.m, p.action, settings, secrets, Options{MaxRecords: p.maxRecords})
	if err != nil {
		return fmt.Errorf("%s.%s: %w", p.m.Name, p.action, err)
	}

	p.engine = e

	return nil
}

func (p *SourcePackage) Process(ctx context.Context, _ <-chan contract.Batch, out chan<- contract.Batch) error {
	return p.engine.Run(ctx, func(recs []record.Record) error {
		select {
		case out <- contract.Batch(recs):
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	})
}
