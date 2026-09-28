package pipeline

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/datasplice-labs/datasplice-core/internal/config"
	thirdparty "github.com/datasplice-labs/datasplice-core/internal/registry"
)

// cacheThirdParty simulates what `datasplice get` does — writes a cached
// manifest and a matching datasplice.lock — without going through
// cmd/get.go (internal/pipeline doesn't depend on cmd). Returns the ref.
func cacheThirdParty(t *testing.T, uses, manifestYAML string) thirdparty.Ref {
	t.Helper()

	home := t.TempDir()
	t.Setenv("DATASPLICE_PATH", home)

	ref, err := thirdparty.ParseRef(uses)
	if err != nil {
		t.Fatalf("ParseRef: %v", err)
	}

	path := thirdparty.CachePath(home, ref)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(manifestYAML), 0o644); err != nil {
		t.Fatal(err)
	}

	lock := &thirdparty.Lockfile{Entries: map[thirdparty.Ref]string{}}
	hash, err := thirdparty.Get(context.Background(), &thirdparty.Fetcher{}, home, ref) // reads the cache we just wrote, no network
	if err != nil {
		t.Fatalf("hashing cached manifest: %v", err)
	}
	lock.Entries[ref] = hash

	if err := lock.Save(thirdparty.LockFile); err != nil {
		t.Fatal(err)
	}

	return ref
}

func thirdPartyManifest(base string) string {
	return `
name: things
version: "0.1.0"
manifest_version: 1
base_url: "` + base + `"
actions:
  list:
    role: source
    method: GET
    path: /items
    records: $.items
`
}

// T6.8, through the real pipeline: a third-party ref not in
// datasplice.lock fails with an actionable message.
func TestThirdPartyRefMissingFromLockFailsBuild(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("DATASPLICE_PATH", t.TempDir())

	m := &config.Main{
		Name: "x",
		Steps: []config.Step{
			{Uses: "github.com/myorg/pkg@v0.2.0", Action: "list"},
			{Uses: "datasplice/csv@latest", With: map[string]any{"path": "out.csv"}},
		},
	}

	_, err := Build(m, nil)
	if err == nil || !strings.Contains(err.Error(), "datasplice get") {
		t.Fatalf("err = %v, want it to suggest `datasplice get`", err)
	}
}

// T6.5, through the real pipeline: a tampered cache fails Build with a
// hash-mismatch error, not silently running stale/edited content.
func TestThirdPartyHashMismatchFailsBuild(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	ref := cacheThirdParty(t, "github.com/myorg/pkg@v0.2.0", thirdPartyManifest("http://example.invalid"))

	// Tamper with the cached file after it was locked.
	home := os.Getenv("DATASPLICE_PATH")
	if err := os.WriteFile(thirdparty.CachePath(home, ref), []byte("tampered: true\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	m := &config.Main{
		Name: "x",
		Steps: []config.Step{
			{Uses: ref.String(), Action: "list"},
			{Uses: "datasplice/csv@latest", With: map[string]any{"path": "out.csv"}},
		},
	}

	_, err := Build(m, nil)
	if err == nil || !strings.Contains(err.Error(), "does not match datasplice.lock") {
		t.Fatalf("err = %v, want a hash-mismatch error", err)
	}
}

// Full path: get (simulated), then a real Build/Configure/Run through a
// third-party ref, end to end.
func TestThirdPartySourceEndToEnd(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"items": []any{map[string]any{"n": 1}, map[string]any{"n": 2}}})
	}))
	defer srv.Close()

	dir := t.TempDir()
	t.Chdir(dir)

	ref := cacheThirdParty(t, "github.com/myorg/pkg@v0.2.0", thirdPartyManifest(srv.URL))

	out := filepath.Join(dir, "out.csv")
	m := &config.Main{
		Name: "x",
		Steps: []config.Step{
			{Uses: ref.String(), Action: "list"},
			{Uses: "datasplice/csv@latest", With: map[string]any{"path": out}},
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

	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "n\n1\n2\n" {
		t.Fatalf("csv = %q", got)
	}
}
