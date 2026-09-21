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
	ID   string
	Uses string
	Pkg  contract.Package
	// Describe is the package's full capability set. Role is the one role
	// this step actually plays, derived from its position (a package that
	// declares more than one, like json, has no other way to say which).
	Describe contract.Describe
	Role     contract.Role
	With     map[string]any
	Secrets  map[string]string
}

// Build resolves every step's package, interpolates its settings, and
// checks the pipeline shape rules: exactly 1 source first, 1 sink last, transforms in between.
func Build(m *config.Main, secretValues map[string]string) ([]Step, error) {
	steps := make([]Step, len(m.Steps))

	// Looping over the steps in order is important for two reasons:
	// 1. The first and last steps must be source and sink, respectively.
	// 2. The last step's export mustn't be invalid, so we need to know which step is last.
	for i, s := range m.Steps {
		// This resolves the package
		p, err := resolveStep(s)
		if err != nil {
			return nil, fmt.Errorf("step %d (%s): %w", i+1, s.Uses, err)
		}

		// We interpolate secrets
		with, err := config.Interpolate(s.With, secretValues)
		if err != nil {
			return nil, fmt.Errorf("step %d (%s): %w", i+1, s.Uses, err)
		}

		// Packages that can check themselves offline (manifest packages)
		// do it here, so `validate` and `plan` catch a bad `with:` or a
		// missing `secrets:` entry before anything runs.
		if v, ok := p.(stepValidator); ok {
			if err := v.Validate(with, s.Secrets); err != nil {
				return nil, fmt.Errorf("step %d (%s): %w", i+1, s.Uses, err)
			}
		}

		// The last step cannot have an export, because there is nothing downstream to receive it.
		// The export is only valid on transform steps, which must be in the middle of the pipeline.
		// In the last step, we only reference an "output", which is not an export, but rather the final output of the pipeline.
		if s.Export != nil && i == len(m.Steps)-1 {
			return nil, fmt.Errorf("step %d (%s): export is not valid on the last step — nothing downstream to receive it", i+1, s.Uses)
		}

		pkg := p
		if s.Export != nil {
			pkg = newExportingPackage(p, s.Export)
		}

		d := p.Describe()
		steps[i] = Step{
			ID:       fmt.Sprintf("%d-%s", i+1, d.Name),
			Uses:     s.Uses,
			Pkg:      pkg,
			Describe: d,
			Role:     positionRole(i, len(m.Steps)),
			With:     with,
			Secrets:  config.StepSecrets(s, secretValues),
		}
	}

	// We have built the steps package. Now we check its shape and rules
	if err := checkShape(steps); err != nil {
		return nil, err
	}

	return steps, nil
}

// positionRole is the role a step must play given its position: first is
// always source, last is always sink, everything between is transform.
// checkShape verifies the resolved package actually supports it.
func positionRole(i, n int) contract.Role {
	switch i {
	case 0:
		return contract.RoleSource
	case n - 1:
		return contract.RoleSink
	default:
		return contract.RoleTransform
	}
}

func checkShape(steps []Step) error {
	n := len(steps)
	if !steps[0].Describe.HasRole(contract.RoleSource) {
		return fmt.Errorf("step 1 (%s) must be a source, got %s", steps[0].Uses, steps[0].Describe.RolesString())
	}

	if !steps[n-1].Describe.HasRole(contract.RoleSink) {
		return fmt.Errorf("step %d (%s) must be a sink, got %s", n, steps[n-1].Uses, steps[n-1].Describe.RolesString())
	}

	for i := 1; i < n-1; i++ {
		if !steps[i].Describe.HasRole(contract.RoleTransform) {
			return fmt.Errorf("step %d (%s) must be a transform, got %s", i+1, steps[i].Uses, steps[i].Describe.RolesString())
		}
	}

	return nil
}

// Configure calls Configure exactly once per step, in order.
func Configure(steps []Step) error {
	for _, s := range steps {
		if err := s.Pkg.Configure(s.With, s.Secrets); err != nil {
			return fmt.Errorf("%s: %w", s.ID, err)
		}
	}
	return nil
}

// Run wires each step's Process to the next over channels and waits for
// all of them. The sink closing its output — implicit here when its
// Process returns — is the commit signal.
// Any step's error cancels the shared context, which every other step's
// Process must observe to unblock its channel sends. The returned count is
// how many records reached the sink — 0 alongside a non-nil error.
func Run(ctx context.Context, steps []Step) (count int, err error) {
	// we cancel() whenever we return, and if any step's goroutine below errors
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Copy steps before mutating it. Steps is caller-owned
	// Build's caller may reuse the same slice for another Run (e.g. run vs. --dry-run),
	// so we shouldn't mutate their copy in place
	steps = append([]Step{}, steps...)
	last := len(steps) - 1

	// count is a named return value; countingSink writes into it via the
	// pointer as records flow through, so it's already populated by the
	// time this function returns
	steps[last].Pkg = &countingSink{inner: steps[last].Pkg, count: &count}

	// One channel between each pair of adjacent steps: step i writes to chans[i],
	// step i+1 reads from it. Buffered (4) so a step doesn't have to wait for
	// its downstream neighbour to be ready for every single batch.
	// This is the only backpressure/decoupling mechanism between steps right now
	chans := make([]chan contract.Batch, len(steps)-1)
	for i := range chans {
		chans[i] = make(chan contract.Batch, 4)
	}

	// Run every step concurrently, each in its own goroutine, wired to
	// its neighbours via the channels above. wg tracks when they've all
	// finished; errs collects whichever ones failed (buffered to the
	// number of steps so no goroutine can block trying to report an
	// error, even if several fail at once).
	var wg sync.WaitGroup
	errs := make(chan error, len(steps))
	for i, s := range steps {
		var in <-chan contract.Batch
		var out chan<- contract.Batch
		if i > 0 {
			in = chans[i-1] // every step but the first reads from the previous step's channel
		}
		if i < len(steps)-1 {
			out = chans[i] // every step but the last writes to its own channel
		}

		wg.Add(1)
		go func(s Step, in <-chan contract.Batch, out chan<- contract.Batch) {
			defer wg.Done()
			// Closing `out` is how this step tells the next one "no more batches coming"
			// the next step's `for batch := range in`(or equivalent select loop) sees
			// the channel close and knows to stop. Only steps with a downstream neighbour
			// (out != nil) need to do this; the last step's out is nil.
			if out != nil {
				defer close(out)
			}

			if err := s.Pkg.Process(ctx, in, out); err != nil {
				errs <- fmt.Errorf("%s: %w", s.ID, err)
				cancel() // stop every other step, not just this one
			}
		}(s, in, out)
	}

	// Block until every step's goroutine has returned (successfully or
	// not) before looking at results.
	wg.Wait()
	close(errs)
	for e := range errs {
		if e != nil {
			return 0, e
		}
	}
	return count, nil
}

// RunDryRun runs the full pipeline but swaps the sink for a counter that
// keeps a small sample instead of writing (datasplice-core-prd.md §3).
func RunDryRun(ctx context.Context, steps []Step) (count int, sample []record.Record, err error) {
	c := &dryRunSink{}
	// Take every step except the real sink, then append our own
	// dryRunSink in its place — same position (last), so it still gets
	// wired up as the sink by Run below, it just doesn't write anywhere.
	dryRunSinkSteps := append([]Step{}, steps[:len(steps)-1]...)
	dryRunSinkSteps = append(dryRunSinkSteps, Step{
		ID: steps[len(steps)-1].ID, Uses: "dry-run", Pkg: c,
		Describe: contract.Describe{Name: "dry-run", Roles: []contract.Role{contract.RoleSink}},
		Role:     contract.RoleSink,
	})

	// Run also wraps our dryRunSink in its own countingSink internally,
	// which would double-count — we ignore that returned count and use
	// c.count (dryRunSink's own tally) instead, since dryRunSink also
	// keeps the sample, and we want both numbers from the same source.
	_, err = Run(ctx, dryRunSinkSteps)

	return c.count, c.sample, err
}

const dryRunSampleSize = 5

type dryRunSink struct {
	count  int
	sample []record.Record
}

func (d *dryRunSink) Describe() contract.Describe {
	return contract.Describe{Name: "dry-run", Roles: []contract.Role{contract.RoleSink}}
}

func (d *dryRunSink) Configure(map[string]any, map[string]string) error { return nil }

func (d *dryRunSink) Process(ctx context.Context, in <-chan contract.Batch, out chan<- contract.Batch) error {
	// A sink's Process just needs to keep draining `in` until it's
	// closed (upstream is done) or ctx is cancelled (something else
	// failed). No out to write to here — dryRunSink never emits anything.
	for {
		select {
		case batch, ok := <-in:
			if !ok {
				return nil // upstream closed cleanly: we're done
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
