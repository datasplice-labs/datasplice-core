package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/datasplice-labs/datasplice-core/internal/contract"
	"github.com/datasplice-labs/datasplice-core/internal/record"
)

// defaultBatchSize mirrors the core's preferred batch_size default from
// datasplice-protocol.md §3.
const defaultBatchSize = 500

// JSON is a source-or-sink builtin: with.path (required) is a file that,
// as a source, must contain a JSON array of objects, and as a sink, is
// (over)written with one. Which one a step plays isn't a setting — it's
// the step's position (see contract.Role's doc comment): Process gets a
// nil `in` when it's first, a nil `out` when it's last.
type JSON struct {
	path   string
	atomic bool
}

func NewJSON() *JSON { return &JSON{} }

func (j *JSON) Describe() contract.Describe {
	return contract.Describe{
		Name: "json", Version: "0.1.0", Roles: []contract.Role{contract.RoleSource, contract.RoleSink},
		Settings: []contract.SettingSpec{
			{Key: "path", Type: "string", Required: true},
			{Key: "atomic", Type: "bool"},
		},
	}
}

func (j *JSON) Configure(settings map[string]any, fn string, on []string, secrets map[string]string) error {
	path, _ := settings["path"].(string)
	if path == "" {
		return fmt.Errorf("json: `with.path` is required")
	}

	j.path = path
	j.atomic, _ = settings["atomic"].(bool)

	return nil
}

func (j *JSON) Process(ctx context.Context, in <-chan contract.Batch, out chan<- contract.Batch) error {
	if in == nil {
		return j.read(ctx, out)
	}

	return j.write(ctx, in)
}

func (j *JSON) read(ctx context.Context, out chan<- contract.Batch) error {
	data, err := os.ReadFile(j.path)
	if err != nil {
		return fmt.Errorf("json: %w", err)
	}

	var records []record.Record
	if err := json.Unmarshal(data, &records); err != nil {
		return fmt.Errorf("json: %s must be a JSON array of objects: %w", j.path, err)
	}

	for i := 0; i < len(records); i += defaultBatchSize {
		end := min(i+defaultBatchSize, len(records))
		select {
		case out <- contract.Batch(records[i:end]):
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	return nil
}

// write buffers every record before marshalling, since a JSON array needs
// a closing bracket only the sink's last write could provide.
func (j *JSON) write(ctx context.Context, in <-chan contract.Batch) error {
	var records []record.Record
	for {
		select {
		case batch, ok := <-in:
			if !ok {
				data, err := json.Marshal(records)
				if err != nil {
					return fmt.Errorf("json: %w", err)
				}

				if j.atomic {
					if err := atomicWriteFile(j.path, data); err != nil {
						return fmt.Errorf("json: %w", err)
					}

					return nil
				}

				if err := os.WriteFile(j.path, data, 0o644); err != nil {
					return fmt.Errorf("json: %w", err)
				}

				return nil
			}
			records = append(records, batch...)
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}
