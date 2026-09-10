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

// JSON is a source builtin: with.path (required) must contain a JSON
// array of objects.
type JSON struct {
	path string
}

func NewJSON() *JSON { return &JSON{} }

func (j *JSON) Describe() contract.Describe {
	return contract.Describe{
		Name: "json", Version: "0.1.0", Role: contract.RoleSource,
		Settings: []contract.SettingSpec{{Key: "path", Type: "string", Required: true}},
	}
}

func (j *JSON) Configure(settings map[string]any, fn string, on []string, secrets map[string]string) error {
	path, _ := settings["path"].(string)
	if path == "" {
		return fmt.Errorf("json: `with.path` is required")
	}

	j.path = path

	return nil
}

func (j *JSON) Process(ctx context.Context, in <-chan contract.Batch, out chan<- contract.Batch) error {
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
