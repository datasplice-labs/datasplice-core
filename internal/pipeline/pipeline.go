// Package pipeline resolves a config.Main into runnable steps and
// executes them: role composition, spawn (in-process, for now), stream
// wiring, and cancellation — datasplice-core-prd.md §6.
package pipeline

import (
	"context"
	"fmt"
	"sync"

	"github.com/datasplice-labs/datasplice-core/internal/config"
	"github.com/datasplice-labs/datasplice-core/internal/contract"
	"github.com/datasplice-labs/datasplice-core/internal/record"
)

// Step is one resolved, described stage — a package instance plus its
// interpolated settings, ready for Configure.
type Step struct {
	ID       string
	Uses     string
	Pkg      contract.Package
	Describe contract.Describe
	With     map[string]any
	Fn       string
	On       []string
	Secrets  map[string]string
}

// Build resolves every step's package, interpolates its settings, and
// checks the pipeline shape rules (datasplice-core-prd.md §2): exactly
// one source first, one sink last, transforms in between.
func Build(m *config.Main, secretValues map[string]string) ([]Step, error) {
	steps := make([]Step, len(m.Steps))
	for i, s := range m.Steps {
		p, err := resolve(s.Uses)
		if err != nil {
			return nil, fmt.Errorf("step %d (%s): %w", i+1, s.Uses, err)
		}

		with, err := config.Interpolate(s.With, secretValues)
		if err != nil {
			return nil, fmt.Errorf("step %d (%s): %w", i+1, s.Uses, err)
		}

		d := p.Describe()
		steps[i] = Step{
			ID:       fmt.Sprintf("%d-%s", i+1, d.Name),
			Uses:     s.Uses,
			Pkg:      p,
			Describe: d,
			With:     with,
			Fn:       s.Fn,
			On:       s.On,
			Secrets:  config.ReferencedValues(s.With, secretValues),
		}
	}
	if err := checkShape(steps); err != nil {
		return nil, err
	}
	if err := checkFunctions(steps); err != nil {
		return nil, err
	}
	return steps, nil
}

func checkShape(steps []Step) error {
	n := len(steps)
	if steps[0].Describe.Role != contract.RoleSource {
		return fmt.Errorf("step 1 (%s) must be a source, got %s", steps[0].Uses, steps[0].Describe.Role)
	}
	if steps[n-1].Describe.Role != contract.RoleSink {
		return fmt.Errorf("step %d (%s) must be a sink, got %s", n, steps[n-1].Uses, steps[n-1].Describe.Role)
	}
	for i := 1; i < n-1; i++ {
		if steps[i].Describe.Role != contract.RoleTransform {
			return fmt.Errorf("step %d (%s) must be a transform, got %s", i+1, steps[i].Uses, steps[i].Describe.Role)
		}
	}
	return nil
}

func checkFunctions(steps []Step) error {
	for i, s := range steps {
		if s.Describe.Role != contract.RoleTransform {
			if s.Fn != "" || len(s.On) > 0 {
				return fmt.Errorf("step %d (%s): `fn`/`on` are only valid on transform steps", i+1, s.Uses)
			}
			continue
		}
		if s.Fn == "" {
			continue
		}
		found := false
		for _, f := range s.Describe.Functions {
			if f.Name == s.Fn {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("step %d (%s): unknown fn %q", i+1, s.Uses, s.Fn)
		}
	}
	return nil
}

// Configure calls Configure exactly once per step, in order.
func Configure(steps []Step) error {
	for _, s := range steps {
		if err := s.Pkg.Configure(s.With, s.Fn, s.On, s.Secrets); err != nil {
			return fmt.Errorf("%s: %w", s.ID, err)
		}
	}
	return nil
}

// Run wires each step's Process to the next over channels and waits for
// all of them. The sink closing its output — implicit here when its
// Process returns — is the commit signal (datasplice-protocol.md §4).
// Any step's error cancels the shared context, which every other step's
// Process must observe to unblock its channel sends.
func Run(ctx context.Context, steps []Step) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	chans := make([]chan contract.Batch, len(steps)-1)
	for i := range chans {
		chans[i] = make(chan contract.Batch, 4)
	}

	var wg sync.WaitGroup
	errs := make(chan error, len(steps))
	for i, s := range steps {
		var in <-chan contract.Batch
		var out chan<- contract.Batch
		if i > 0 {
			in = chans[i-1]
		}
		if i < len(steps)-1 {
			out = chans[i]
		}

		wg.Add(1)
		go func(s Step, in <-chan contract.Batch, out chan<- contract.Batch) {
			defer wg.Done()
			if out != nil {
				defer close(out)
			}
			if err := s.Pkg.Process(ctx, in, out); err != nil {
				errs <- fmt.Errorf("%s: %w", s.ID, err)
				cancel()
			}
		}(s, in, out)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}

// RunDryRun runs the full pipeline but swaps the sink for a counter that
// keeps a small sample instead of writing (datasplice-core-prd.md §3).
func RunDryRun(ctx context.Context, steps []Step) (count int, sample []record.Record, err error) {
	c := &dryRunSink{}
	dsSteps := append([]Step{}, steps[:len(steps)-1]...)
	dsSteps = append(dsSteps, Step{
		ID: steps[len(steps)-1].ID, Uses: "dry-run", Pkg: c,
		Describe: contract.Describe{Name: "dry-run", Role: contract.RoleSink},
	})
	err = Run(ctx, dsSteps)
	return c.count, c.sample, err
}

const dryRunSampleSize = 5

type dryRunSink struct {
	count  int
	sample []record.Record
}

func (d *dryRunSink) Describe() contract.Describe {
	return contract.Describe{Name: "dry-run", Role: contract.RoleSink}
}

func (d *dryRunSink) Configure(map[string]any, string, []string, map[string]string) error { return nil }

func (d *dryRunSink) Process(ctx context.Context, in <-chan contract.Batch, out chan<- contract.Batch) error {
	for {
		select {
		case batch, ok := <-in:
			if !ok {
				return nil
			}
			d.count += len(batch)
			for _, r := range batch {
				if len(d.sample) < dryRunSampleSize {
					d.sample = append(d.sample, r)
				}
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}
