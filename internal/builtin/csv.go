package builtin

import (
	"context"
	"encoding/csv"
	"fmt"
	"os"
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
	path string
}

func NewCSV() *CSV { return &CSV{} }

func (c *CSV) Describe() contract.Describe {
	return contract.Describe{
		Name: "csv", Version: "0.1.0", Role: contract.RoleSink,
		Settings: []contract.SettingSpec{{Key: "path", Type: "string", Required: true}},
	}
}

func (c *CSV) Configure(settings map[string]any, fn string, on []string, secrets map[string]string) error {
	path, ok := settings["path"].(string)
	if !ok {
		return fmt.Errorf("csv: `with.path` must be a string")
	}

	if path == "" {
		return fmt.Errorf("csv: `with.path` is required")
	}

	c.path = path

	return nil
}

func (c *CSV) Process(ctx context.Context, in <-chan contract.Batch, out chan<- contract.Batch) error {
	f, err := os.Create(c.path)
	if err != nil {
		return fmt.Errorf("csv: %w", err)
	}

	defer f.Close()

	w := csv.NewWriter(f)
	defer w.Flush()

	var header []string
	for {
		select {
		case batch, ok := <-in:
			if !ok {
				return w.Error()
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
