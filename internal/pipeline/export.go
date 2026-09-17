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
func (e *exportingPackage) processSource(ctx context.Context, out chan<- contract.Batch) error {
	innerOut := make(chan contract.Batch, exportBufSize)
	errCh := make(chan error, 1)
	go func() {
		err := e.inner.Process(ctx, nil, innerOut)
		close(innerOut)
		errCh <- err
	}()

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
	return <-errCh
}

// processTransform tees the real input so each produced batch can be
// paired, by index, with the batch the inner package received for it.
func (e *exportingPackage) processTransform(ctx context.Context, in <-chan contract.Batch, out chan<- contract.Batch) error {
	innerIn := make(chan contract.Batch, exportBufSize)
	innerOut := make(chan contract.Batch, exportBufSize)
	queued := make(chan contract.Batch, exportBufSize)

	go func() {
		defer close(innerIn)
		for {
			select {
			case b, ok := <-in:
				if !ok {
					return
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

	errCh := make(chan error, 1)
	go func() {
		err := e.inner.Process(ctx, innerIn, innerOut)
		close(innerOut)
		errCh <- err
	}()

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
