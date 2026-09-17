package pipeline

import (
	"context"
	"fmt"

	"github.com/datasplice-labs/datasplice-core/internal/config"
	"github.com/datasplice-labs/datasplice-core/internal/contract"
)

// exportBufSize matches the buffer size Run gives every inter-step
// channel (see chans in Run).
const exportBufSize = 4

// exportingPackage wraps a contract.Package so export.Apply runs on every
// record it produces, without any builtin needing to know about export:.
type exportingPackage struct {
	inner  contract.Package
	export *config.Export
}

func newExportingPackage(inner contract.Package, export *config.Export) contract.Package {
	return &exportingPackage{inner: inner, export: export}
}

func (e *exportingPackage) Describe() contract.Describe { return e.inner.Describe() }

func (e *exportingPackage) Configure(settings map[string]any, secrets map[string]string) error {
	return e.inner.Configure(settings, secrets)
}

func (e *exportingPackage) Process(ctx context.Context, in <-chan contract.Batch, out chan<- contract.Batch) error {
	if in == nil {
		return e.processSource(ctx, out)
	}
	return e.processTransform(ctx, in, out)
}

// processSource has no upstream record to call "in" — the record the
// source produced stands in for both scopes (file-structure.md: "an
// import step has no out: it *is* the out").
//
// We can't run e.export.Apply on the inner package's own output channel
// directly, because the inner package (e.g. http, json) doesn't know
// anything about export: it just writes to whatever channel it's given.
// So we give it a private channel (innerOut) that only this function
// reads from, apply the export mapping to each batch as it arrives, and
// forward the *result* to the real out channel the rest of the pipeline
// is reading from.
func (e *exportingPackage) processSource(ctx context.Context, out chan<- contract.Batch) error {
	innerOut := make(chan contract.Batch, exportBufSize)

	// Run the wrapped package in its own goroutine so this function is
	// free to drain innerOut concurrently. Otherwise, if the inner
	// package's Process blocks trying to send and nothing is reading
	// innerOut yet, we'd deadlock before ever getting here.
	// errCh is buffered so that goroutine never blocks on the send even
	// if this function has already returned (e.g. because ctx was
	// cancelled while we were still waiting on the `out <-` below).
	errCh := make(chan error, 1)
	go func() {
		err := e.inner.Process(ctx, nil, innerOut)
		close(innerOut) // tells the `for produced := range innerOut` loop below to stop
		errCh <- err
	}()

	// Read every batch the inner package produced, apply the export
	// mapping, and pass it on. This loop ends when the goroutine above
	// closes innerOut (inner package is done, err or not).
	for produced := range innerOut {
		exported, err := e.applyBatch(produced, nil)
		if err != nil {
			return err
		}

		select {
		case out <- exported:
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	// innerOut is closed, so the inner Process call has already returned
	// and written its result to errCh — this receive won't block.
	return <-errCh
}

// processTransform is the same idea as processSource, but a transform
// step also has an upstream `in` to deal with: export.Apply needs the
// *pre-transform* record ("in.foo") as well as what the step produced
// ("out.bar"), and the inner package only ever sees one side of that (the
// record it's currently transforming). So instead of just forwarding `in`
// straight to the inner package, we tee it: every batch that comes in
// is both (a) handed to the inner package to transform, and (b) kept on
// the side in `queued` so we can pair it back up with whatever the inner
// package produces from it, batch for batch.
func (e *exportingPackage) processTransform(ctx context.Context, in <-chan contract.Batch, out chan<- contract.Batch) error {
	innerIn := make(chan contract.Batch, exportBufSize)  // feeds the wrapped package, same content as `in`
	innerOut := make(chan contract.Batch, exportBufSize) // wrapped package's raw output, before export.Apply
	queued := make(chan contract.Batch, exportBufSize)   // the "in" side kept aside, so we can match it to what comes out

	// Tee goroutine: reads once from the real `in`, writes the same batch
	// to both innerIn (for the inner package to consume) and queued (for
	// us to remember). Both sends are individually select'd on ctx.Done()
	// so a cancellation can't get stuck if either downstream reader has
	// already stopped listening.
	go func() {
		defer close(innerIn) // signals the inner package that input is exhausted
		for {
			select {
			case b, ok := <-in:
				if !ok {
					return // upstream closed: nothing left to tee
				}
				select {
				case innerIn <- b:
				case <-ctx.Done():
					return
				}
				select {
				case queued <- b:
				case <-ctx.Done():
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	// Run the inner package against innerIn/innerOut in its own goroutine,
	// same reasoning as processSource: it needs to run concurrently with
	// us draining innerOut, or we'd deadlock.
	errCh := make(chan error, 1)
	go func() {
		err := e.inner.Process(ctx, innerIn, innerOut)
		close(innerOut)
		errCh <- err
	}()

	// For every batch the inner package produced, pop the matching batch
	// it was given (queued, in the same order. The inner package is
	// assumed to process one batch in, one batch out, in order; exportingPackage)
	// apply the export mapping using both sides, and forward the result.
	for produced := range innerOut {
		var received contract.Batch
		select {
		case received = <-queued:
		case <-ctx.Done():
			return ctx.Err()
		}

		exported, err := e.applyBatch(produced, received)
		if err != nil {
			return err
		}
		select {
		case out <- exported:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return <-errCh
}

func (e *exportingPackage) applyBatch(produced, received contract.Batch) (contract.Batch, error) {
	if received != nil && len(received) != len(produced) {
		return nil, fmt.Errorf("export: step produced %d records for %d received; export requires a 1:1 transform", len(produced), len(received))
	}

	result := make(contract.Batch, len(produced))
	for i, out := range produced {
		in := out
		if received != nil {
			in = received[i]
		}
		rec, err := e.export.Apply(in, out)
		if err != nil {
			return nil, err
		}
		result[i] = rec
	}
	return result, nil
}
