package builtin

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/datasplice-labs/datasplice-core/internal/contract"
	"github.com/datasplice-labs/datasplice-core/internal/record"
)

func TestCSVAtomicWriteSucceeds(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.csv")

	c := NewCSV()
	if err := c.Configure(map[string]any{"path": path, "atomic": true}, "", nil, nil); err != nil {
		t.Fatalf("Configure: %v", err)
	}

	in := make(chan contract.Batch, 1)
	in <- contract.Batch{record.Record{"id": 1.0}}
	close(in)

	if err := c.Process(context.Background(), in, nil); err != nil {
		t.Fatalf("Process: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading output: %v", err)
	}
	if string(got) != "id\n1\n" {
		t.Fatalf("csv output = %q", got)
	}
	assertNoLeftoverTempFile(t, dir)
}

// TestCSVAtomicWriteFailureLeavesNoFile is T3.15's "done when": a mid-run
// failure leaves no output file.
func TestCSVAtomicWriteFailureLeavesNoFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.csv")

	c := NewCSV()
	if err := c.Configure(map[string]any{"path": path, "atomic": true}, "", nil, nil); err != nil {
		t.Fatalf("Configure: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	in := make(chan contract.Batch)
	go func() {
		in <- contract.Batch{record.Record{"id": 1.0}}
		cancel()
	}()

	if err := c.Process(ctx, in, nil); err == nil {
		t.Fatalf("expected an error from context cancellation mid-write")
	}

	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Fatalf("atomic csv write must leave no output file on failure, stat err = %v", statErr)
	}
	assertNoLeftoverTempFile(t, dir)
}

func assertNoLeftoverTempFile(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading dir: %v", err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".datasplice-tmp-") {
			t.Fatalf("leftover temp file: %s", e.Name())
		}
	}
}
