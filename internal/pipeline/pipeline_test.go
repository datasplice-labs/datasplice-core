package pipeline

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/datasplice-labs/datasplice-core/internal/config"
)

// TestEndToEndJSONMapCSV is the M0 exit criterion from
// datasplice-core-prd.md §8: a real pipeline runs end to end from a real
// config file through actual builtins, no mocks.
func TestEndToEndJSONMapCSV(t *testing.T) {
	dir := t.TempDir()
	inPath := filepath.Join(dir, "in.json")
	outPath := filepath.Join(dir, "out.csv")

	if err := os.WriteFile(inPath, []byte(`[
		{"id": 1, "name": "Ada", "extra": "drop me"},
		{"id": 2, "name": "Grace", "extra": "drop me too"}
	]`), 0o644); err != nil {
		t.Fatal(err)
	}

	m := &config.Main{
		Name: "test",
		Steps: []config.Step{
			{Uses: "datasplice/json@latest", With: map[string]any{"path": inPath}},
			{Uses: "datasplice/map@latest", With: map[string]any{"select": []any{"id", "name"}}},
			{Uses: "datasplice/csv@latest", With: map[string]any{"path": outPath}},
		},
	}

	steps, err := Build(m, nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if err := Configure(steps); err != nil {
		t.Fatalf("Configure: %v", err)
	}
	if err := Run(context.Background(), steps); err != nil {
		t.Fatalf("Run: %v", err)
	}

	got, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("reading output: %v", err)
	}
	want := "id,name\n1,Ada\n2,Grace\n"
	if string(got) != want {
		t.Fatalf("csv output = %q, want %q", got, want)
	}
}

func TestBuildRejectsBadShape(t *testing.T) {
	m := &config.Main{
		Name: "bad",
		Steps: []config.Step{
			{Uses: "datasplice/csv@latest", With: map[string]any{"path": "x.csv"}}, // sink first: wrong
			{Uses: "datasplice/map@latest"},
		},
	}
	if _, err := Build(m, nil); err == nil {
		t.Fatalf("expected shape error when a sink is first")
	}
}

func TestRunDryRunDoesNotWriteSink(t *testing.T) {
	dir := t.TempDir()
	inPath := filepath.Join(dir, "in.json")
	outPath := filepath.Join(dir, "out.csv")
	os.WriteFile(inPath, []byte(`[{"id": 1}, {"id": 2}, {"id": 3}]`), 0o644)

	m := &config.Main{
		Name: "dry",
		Steps: []config.Step{
			{Uses: "datasplice/json@latest", With: map[string]any{"path": inPath}},
			{Uses: "datasplice/csv@latest", With: map[string]any{"path": outPath}},
		},
	}
	steps, err := Build(m, nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if err := Configure(steps); err != nil {
		t.Fatalf("Configure: %v", err)
	}
	count, sample, err := RunDryRun(context.Background(), steps)
	if err != nil {
		t.Fatalf("RunDryRun: %v", err)
	}
	if count != 3 {
		t.Fatalf("count = %d, want 3", count)
	}
	if len(sample) != 3 {
		t.Fatalf("sample = %d records, want 3", len(sample))
	}
	if _, err := os.Stat(outPath); err == nil {
		t.Fatalf("dry run must not write the sink file")
	}
}
