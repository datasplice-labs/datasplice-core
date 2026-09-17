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
	count, err := Run(context.Background(), steps)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if count != 2 {
		t.Fatalf("count = %d, want 2", count)
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

// TestEndToEndJSONAsSink covers datasplice-prd-tasks.md T3.10: json can
// act as the last step (sink), not just the first (source) — the same
// package, chosen by position, per contract.Role's doc comment.
func TestEndToEndJSONAsSink(t *testing.T) {
	dir := t.TempDir()
	inPath := filepath.Join(dir, "in.json")
	outPath := filepath.Join(dir, "out.json")

	if err := os.WriteFile(inPath, []byte(`[{"id": 1, "name": "Ada"}]`), 0o644); err != nil {
		t.Fatal(err)
	}

	m := &config.Main{
		Name: "json-sink",
		Steps: []config.Step{
			{Uses: "datasplice/json@latest", With: map[string]any{"path": inPath}},
			{Uses: "datasplice/json@latest", With: map[string]any{"path": outPath}},
		},
	}

	steps, err := Build(m, nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if err := Configure(steps); err != nil {
		t.Fatalf("Configure: %v", err)
	}
	if _, err := Run(context.Background(), steps); err != nil {
		t.Fatalf("Run: %v", err)
	}

	got, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("reading output: %v", err)
	}
	want := `[{"id":1,"name":"Ada"}]`
	if string(got) != want {
		t.Fatalf("json output = %q, want %q", got, want)
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

func TestBuildRejectsExportOnLastStep(t *testing.T) {
	m := &config.Main{
		Name: "bad-export",
		Steps: []config.Step{
			{Uses: "datasplice/json@latest", With: map[string]any{"path": "x.json"}},
			{Uses: "datasplice/csv@latest", With: map[string]any{"path": "x.csv"}, Export: &config.Export{Passthrough: true}},
		},
	}
	if _, err := Build(m, nil); err == nil {
		t.Fatalf("expected error for export on the last step")
	}
}

func TestExportOnSourceStep(t *testing.T) {
	dir := t.TempDir()
	inPath := filepath.Join(dir, "in.json")
	outPath := filepath.Join(dir, "out.csv")
	if err := os.WriteFile(inPath, []byte(`[{"id": 1, "extra": "drop me"}]`), 0o644); err != nil {
		t.Fatal(err)
	}

	m := &config.Main{
		Name: "export-source",
		Steps: []config.Step{
			{
				Uses: "datasplice/json@latest",
				With: map[string]any{"path": inPath},
				// no passthrough: only `values` survive, "extra" is dropped
				Export: &config.Export{Values: map[string]string{"identifier": "in.id"}},
			},
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
	if _, err := Run(context.Background(), steps); err != nil {
		t.Fatalf("Run: %v", err)
	}

	got, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("reading output: %v", err)
	}
	want := "identifier\n1\n"
	if string(got) != want {
		t.Fatalf("csv output = %q, want %q", got, want)
	}
}

func TestExportOnTransformStep(t *testing.T) {
	dir := t.TempDir()
	inPath := filepath.Join(dir, "in.json")
	outPath := filepath.Join(dir, "out.csv")
	if err := os.WriteFile(inPath, []byte(`[{"id": 1, "name": "Ada"}]`), 0o644); err != nil {
		t.Fatal(err)
	}

	m := &config.Main{
		Name: "export-transform",
		Steps: []config.Step{
			{Uses: "datasplice/json@latest", With: map[string]any{"path": inPath}},
			{
				Uses: "datasplice/map@latest",
				With: map[string]any{"select": []any{"name"}}, // map itself only keeps "name"
				Export: &config.Export{
					Passthrough: true, // keep what map produced ("name")
					Values:      map[string]string{"original_id": "in.id"}, // pull from the pre-map record
				},
			},
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
	if _, err := Run(context.Background(), steps); err != nil {
		t.Fatalf("Run: %v", err)
	}

	got, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("reading output: %v", err)
	}
	want := "name,original_id\nAda,1\n"
	if string(got) != want {
		t.Fatalf("csv output = %q, want %q", got, want)
	}
}

// TestRunReturnsZeroCountOnError covers Run's documented contract: 0
// alongside a non-nil error, never a partial count.
func TestRunReturnsZeroCountOnError(t *testing.T) {
	dir := t.TempDir()
	inPath := filepath.Join(dir, "in.json")
	if err := os.WriteFile(inPath, []byte(`[{"id": 1}]`), 0o644); err != nil {
		t.Fatal(err)
	}

	m := &config.Main{
		Name: "bad-sink-path",
		Steps: []config.Step{
			{Uses: "datasplice/json@latest", With: map[string]any{"path": inPath}},
			// a directory can't be opened as a file: Process fails immediately
			{Uses: "datasplice/csv@latest", With: map[string]any{"path": dir}},
		},
	}

	steps, err := Build(m, nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if err := Configure(steps); err != nil {
		t.Fatalf("Configure: %v", err)
	}
	count, err := Run(context.Background(), steps)
	if err == nil {
		t.Fatalf("expected an error writing to a directory path")
	}
	if count != 0 {
		t.Fatalf("count = %d, want 0 alongside an error", count)
	}
}

func TestRunDryRunDoesNotWriteSink(t *testing.T) {
	dir := t.TempDir()
	inPath := filepath.Join(dir, "in.json")
	outPath := filepath.Join(dir, "out.csv")
	if err := os.WriteFile(inPath, []byte(`[{"id": 1}, {"id": 2}, {"id": 3}]`), 0o644); err != nil {
		t.Fatal(err)
	}

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
