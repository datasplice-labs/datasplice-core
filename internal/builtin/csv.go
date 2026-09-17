package builtin

import (
	"context"
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/datasplice-labs/datasplice-core/internal/contract"
	"github.com/datasplice-labs/datasplice-core/internal/record"
)

// CSV is a sink builtin: with.path (required) writes records as CSV.
//
// ponytail: the header is the sorted keys of the first record seen — no
// explicit with.columns config yet. Add it if a source's key order needs
// to be preserved, or if callers need to force a fixed column set.
type CSV struct {
	path   string
	atomic bool
}

func NewCSV() *CSV { return &CSV{} }

func (c *CSV) Describe() contract.Describe {
	return contract.Describe{
		Name: "csv", Version: "0.1.0", Roles: []contract.Role{contract.RoleSink},
		Settings: []contract.SettingSpec{
			{Key: "path", Type: "string", Required: true},
			{Key: "atomic", Type: "bool"},
		},
	}
}

func (c *CSV) Configure(settings map[string]any, secrets map[string]string) error {
	path, ok := settings["path"].(string)
	if !ok {
		return fmt.Errorf("csv: `with.path` must be a string")
	}

	if path == "" {
		return fmt.Errorf("csv: `with.path` is required")
	}

	c.path = path
	c.atomic, _ = settings["atomic"].(bool)

	return nil
}

// writePath is where rows actually land: the real target, or — when
// atomic — a temp file in the same directory that Process renames over
// the target only once every row has been written successfully.
func (c *CSV) Process(ctx context.Context, in <-chan contract.Batch, out chan<- contract.Batch) (err error) {
	// err is a named return so the deferred cleanup below can see whether
	// writing actually succeeded and decide what to do with the file
	// accordingly (rename it into place, or throw it away).
	writePath := c.path
	var f *os.File
	if c.atomic {
		// Write to a throwaway temp file in the same directory first —
		// "same directory" matters because os.Rename below is only
		// guaranteed atomic when source and destination are on the same
		// filesystem, which a same-directory temp file guarantees.
		f, err = os.CreateTemp(filepath.Dir(c.path), ".datasplice-tmp-*")
	} else {
		// Non-atomic: write straight to the real path, no temp file.
		f, err = os.Create(c.path)
	}
	if err != nil {
		return fmt.Errorf("csv: %w", err)
	}

	if c.atomic {
		writePath = f.Name() // the actual temp file path os.CreateTemp picked
	}

	// This defer runs no matter how Process returns (success, a write
	// error, or ctx cancellation) and is what actually finalizes the
	// file: close it, then — only if this was an atomic write — either
	// promote the temp file to the real path (success) or delete it
	// (failure), so a failed atomic write never leaves a half-written
	// file behind and never touches the real path at all.
	defer func() {
		if cerr := f.Close(); err == nil {
			err = cerr
		}

		if !c.atomic {
			return
		}

		if err != nil {
			_ = os.Remove(writePath)
			return
		}

		err = os.Rename(writePath, c.path)
	}()

	w := csv.NewWriter(f)
	defer w.Flush() // runs before the file-close defer above (LIFO), so rows are on disk before we close/rename

	// Main loop: read batches from upstream until it closes `in` (we're
	// done) or ctx is cancelled (something else in the pipeline failed
	// and we should stop too). Every record in every batch becomes one
	// CSV row; the header is taken from whichever record arrives first.
	var header []string
	for {
		select {
		case batch, ok := <-in:
			if !ok {
				return w.Error() // channel closed: upstream is done, report any write error the writer saw
			}
			for _, rec := range batch {
				if header == nil {
					header = sortedKeys(rec)
					if err := w.Write(header); err != nil {
						return fmt.Errorf("csv: %w", err)
					}
				}
				row := make([]string, len(header))
				for i, k := range header {
					if v, ok := rec[k]; ok {
						row[i] = fmt.Sprint(v)
					}
				}
				if err := w.Write(row); err != nil {
					return fmt.Errorf("csv: %w", err)
				}
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func sortedKeys(r record.Record) []string {
	keys := make([]string, 0, len(r))
	for k := range r {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
