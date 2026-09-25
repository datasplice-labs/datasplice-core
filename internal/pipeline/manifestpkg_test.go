package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/datasplice-labs/datasplice-core/internal/config"
)

// writeManifest writes a manifest for a "things" API at base and returns
// its path, usable as a step's `uses:`.
func writeManifest(t *testing.T, base string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "datasplice.yaml")
	yaml := fmt.Sprintf(`
name: things
version: "0.1.0"
manifest_version: 1
base_url: %q
settings:
  kind:
    type: string
    required: true
    one_of: [a, b]
  size:
    type: number
secrets:
  TOKEN:
    description: api token
auth:
  type: bearer
  token: "{{ secrets.TOKEN }}"
actions:
  list:
    role: source
    method: GET
    path: "/{{ settings.kind }}"
    records: $.items
    paginate:
      style: page
      param: page
      until: empty
  push:
    role: sink
    method: POST
    path: /push
`, base)

	if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}

	return path
}

// thingsServer serves pages of {"items":[{"n":..}]} for /a, requiring the
// bearer token, and counts requests.
func thingsServer(t *testing.T, pages, perPage int) (*httptest.Server, *int) {
	t.Helper()
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Header.Get("Authorization") != "Bearer tok-123" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		page, err := strconv.Atoi(r.URL.Query().Get("page"))
		if err != nil {
			page = 1
		}

		items := []any{}
		if page <= pages {
			for i := 0; i < perPage; i++ {
				items = append(items, map[string]any{"n": (page-1)*perPage + i})
			}
		}

		_ = json.NewEncoder(w).Encode(map[string]any{"items": items})
	}))
	t.Cleanup(srv.Close)

	return srv, &requests
}

func manifestFlow(uses, out string, step func(*config.Step)) *config.Main {
	src := config.Step{
		Uses:    uses,
		Action:  "list",
		With:    map[string]any{"kind": "a"},
		Secrets: []string{"TOKEN"},
	}
	if step != nil {
		step(&src)
	}

	return &config.Main{
		Name: "things",
		Steps: []config.Step{
			src,
			{Uses: "datasplice/csv@latest", With: map[string]any{"path": out}},
		},
	}
}

// T5.9: a manifest-described endpoint emits records into the pipeline.
func TestManifestSourceEndToEnd(t *testing.T) {
	srv, _ := thingsServer(t, 2, 2)
	out := filepath.Join(t.TempDir(), "out.csv")

	steps, err := Build(manifestFlow(writeManifest(t, srv.URL), out, nil), map[string]string{"TOKEN": "tok-123"})
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

	got, _ := os.ReadFile(out)
	if count != 4 || string(got) != "n\n0\n1\n2\n3\n" {
		t.Fatalf("count = %d, csv = %q", count, got)
	}
}

// T5.18 through the pipeline: max_records stops the run early.
func TestManifestSourceMaxRecords(t *testing.T) {
	srv, requests := thingsServer(t, 10, 5)
	out := filepath.Join(t.TempDir(), "out.csv")

	flow := manifestFlow(writeManifest(t, srv.URL), out, func(s *config.Step) { s.MaxRecords = 7 })
	steps, err := Build(flow, map[string]string{"TOKEN": "tok-123"})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	if err := Configure(steps); err != nil {
		t.Fatalf("Configure: %v", err)
	}

	count, err := Run(context.Background(), steps)
	if err != nil || count != 7 || *requests != 2 {
		t.Fatalf("count = %d, requests = %d, err = %v; want 7 records over 2 requests", count, *requests, err)
	}
}

// datasplice/http is httpengine-backed too (internal/builtin/http.go), so
// it gets the same max_records support as a manifest-file source — the
// same server thingsServer serves for TestManifestSourceMaxRecords above.
func TestHTTPBuiltinAcceptsMaxRecords(t *testing.T) {
	srv, requests := thingsServer(t, 10, 5)
	out := filepath.Join(t.TempDir(), "out.csv")

	m := &config.Main{
		Name: "http-max-records",
		Steps: []config.Step{
			{
				Uses:       "datasplice/http@latest",
				MaxRecords: 7,
				With: map[string]any{
					"url":     srv.URL + "/a",
					"records": "$.items",
					"auth":    map[string]any{"type": "bearer", "token": "tok-123"},
					"paginate": map[string]any{
						"style": "page",
						"param": "page",
						"until": "empty",
					},
				},
			},
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
	if err != nil || count != 7 || *requests != 2 {
		t.Fatalf("count = %d, requests = %d, err = %v; want 7 records over 2 requests", count, *requests, err)
	}
}

// The offline checks fire in Build, so `validate` and `plan` catch them
// before anything runs.
func TestManifestStepValidation(t *testing.T) {
	path := writeManifest(t, "http://localhost")
	secrets := map[string]string{"TOKEN": "tok-123"}

	cases := []struct {
		name string
		step func(*config.Step)
		want string
	}{
		{"unknown action", func(s *config.Step) { s.Action = "nope" }, `no action "nope"`},
		{"action required", func(s *config.Step) { s.Action = "" }, "`action` is required"},
		{"sink action as source", func(s *config.Step) { s.Action = "push" }, "only source actions"},
		{"missing required setting", func(s *config.Step) { s.With = map[string]any{} }, "with.kind is required"},
		{"setting outside one_of", func(s *config.Step) { s.With = map[string]any{"kind": "z"} }, "is not one of"},
		{"wrong setting type", func(s *config.Step) { s.With = map[string]any{"kind": "a", "size": "big"} }, "must be a number"},
		{"secret not granted", func(s *config.Step) { s.Secrets = nil }, `"TOKEN" is required`},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := Build(manifestFlow(path, "out.csv", c.step), secrets)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err = %v, want it to contain %q", err, c.want)
			}
		})
	}
}

// YAML gives whole numbers as int; that must satisfy `type: number`.
func TestManifestSettingAcceptsYAMLInt(t *testing.T) {
	flow := manifestFlow(writeManifest(t, "http://localhost"), "out.csv", func(s *config.Step) {
		s.With = map[string]any{"kind": "a", "size": 10}
	})

	if _, err := Build(flow, map[string]string{"TOKEN": "tok-123"}); err != nil {
		t.Fatalf("Build: %v", err)
	}
}

func TestManifestOnlyFieldsRejectedOnBuiltins(t *testing.T) {
	cases := []struct {
		name string
		step config.Step
		want string
	}{
		{"action", config.Step{Uses: "datasplice/json@latest", Action: "list", With: map[string]any{"path": "x.json"}}, "`action` is only valid"},
		// max_records IS supported on datasplice/http (httpengine-backed,
		// see TestHTTPBuiltinAcceptsMaxRecords) — json has no pagination
		// concept at all, so it stays rejected.
		{"max_records on a non-paginating builtin", config.Step{Uses: "datasplice/json@latest", MaxRecords: 5, With: map[string]any{"path": "x.json"}}, "`max_records` is not supported"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := &config.Main{Name: "x", Steps: []config.Step{c.step, {Uses: "datasplice/csv@latest", With: map[string]any{"path": "o.csv"}}}}
			if _, err := Build(m, nil); err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err = %v, want it to contain %q", err, c.want)
			}
		})
	}
}

// A step only sees the secrets it was granted.
func TestStepSecretsAreScopedToGrants(t *testing.T) {
	step := config.Step{Uses: "x", Secrets: []string{"A"}, With: map[string]any{"k": "${B}"}}
	got := config.StepSecrets(step, map[string]string{"A": "1", "B": "2", "C": "3"})

	if len(got) != 2 || got["A"] != "1" || got["B"] != "2" {
		t.Fatalf("StepSecrets = %v, want only A (granted) and B (referenced)", got)
	}
}
