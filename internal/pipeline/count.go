package pipeline

import (
	"context"

	"github.com/datasplice-labs/datasplice-core/internal/contract"
)

// countingSink wraps the last step's package to tally how many records
// actually reached it, for run's closing summary line
// (datasplice-prd-tasks.md T3.14: "✓ 142 records in 0.4s"). The count
// becomes readable once Process returns: the tee goroutine's last write
// to *count happens-before it closes `counted`, which happens-before
// inner.Process sees end-of-input and returns.
type countingSink struct {
	inner contract.Package
	count *int
}

func (c *countingSink) Describe() contract.Describe { return c.inner.Describe() }

func (c *countingSink) Configure(settings map[string]any, secrets map[string]string) error {
	return c.inner.Configure(settings, secrets)
}

func (c *countingSink) Process(ctx context.Context, in <-chan contract.Batch, out chan<- contract.Batch) error {
	// The real sink (c.inner) doesn't know about counting, so we don't
	// hand it `in` directly. Instead we relay every batch through this
	// goroutine first: it tallies the batch's length into *c.count, then
	// forwards the exact same batch on `counted`, which is what the real
	// sink actually reads from. This is a passthrough, not a transform —
	// the records themselves are never touched, only counted.
	counted := make(chan contract.Batch, exportBufSize)
	go func() {
		defer close(counted) // tells the real sink's Process that input is done
		for {
			select {
			case b, ok := <-in:
				if !ok {
					return // upstream closed, nothing more to relay
				}
				*c.count += len(b)
				select {
				case counted <- b:
				case <-ctx.Done():
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	// Run the real sink directly in this goroutine (no need for a second
	// one here, unlike exportingPackage) — we're not reading anything
	// concurrently on this side, just blocking until it's done.
	return c.inner.Process(ctx, counted, out)
}
