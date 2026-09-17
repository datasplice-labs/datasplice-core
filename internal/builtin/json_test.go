package builtin

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/datasplice-labs/datasplice-core/internal/contract"
	"github.com/datasplice-labs/datasplice-core/internal/record"
)

func TestJSONWriteRoundTripsThroughRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out.json")

	w := NewJSON()
	if err := w.Configure(map[string]any{"path": path}, nil); err != nil {
		t.Fatalf("Configure: %v", err)
	}

	if !w.Describe().HasRole(contract.RoleSink) {
		t.Fatalf("json must be able to act as a sink")
	}

	in := make(chan contract.Batch, 1)
	in <- contract.Batch{record.Record{"id": 1.0}, record.Record{"id": 2.0}}
	close(in)

	if err := w.Process(context.Background(), in, nil); err != nil {
		t.Fatalf("Process (write): %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading output: %v", err)
	}

	if len(data) == 0 {
		t.Fatalf("expected non-empty JSON output")
	}

	r := NewJSON()
	if err := r.Configure(map[string]any{"path": path}, nil); err != nil {
		t.Fatalf("Configure: %v", err)
	}

	out := make(chan contract.Batch, 1)
	if err := r.Process(context.Background(), nil, out); err != nil {
		t.Fatalf("Process (read): %v", err)
	}
	close(out)

	var got []record.Record
	for batch := range out {
		got = append(got, batch...)
	}
	if len(got) != 2 || got[0]["id"] != 1.0 || got[1]["id"] != 2.0 {
		t.Fatalf("round trip = %v", got)
	}
}

func TestJSONAtomicWriteSucceeds(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.json")

	w := NewJSON()
	if err := w.Configure(map[string]any{"path": path, "atomic": true}, nil); err != nil {
		t.Fatalf("Configure: %v", err)
	}

	in := make(chan contract.Batch, 1)
	in <- contract.Batch{record.Record{"id": 1.0}}
	close(in)

	if err := w.Process(context.Background(), in, nil); err != nil {
		t.Fatalf("Process: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading output: %v", err)
	}
	if string(got) != `[{"id":1}]` {
		t.Fatalf("json output = %q", got)
	}
	assertNoLeftoverTempFile(t, dir)
}
