package builtin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/datasplice-labs/datasplice-core/internal/contract"
)

func TestHTTPCursorPaginationAndAuth(t *testing.T) {
	pages := [][]map[string]any{
		{{"id": 1.0}, {"id": 2.0}},
		{{"id": 3.0}},
	}
	requests := 0
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" {
			t.Errorf("missing bearer token, got %q", r.Header.Get("Authorization"))
		}

		idx := requests
		requests++
		body := map[string]any{"items": pages[idx]}
		if idx+1 < len(pages) {
			body["next"] = srv.URL // same stub server serves the "next" page too
		}
		json.NewEncoder(w).Encode(body)
	}))

	defer srv.Close()

	h := NewHTTP()
	if err := h.Configure(map[string]any{
		"url":          srv.URL,
		"auth":         map[string]any{"type": "bearer", "token": "tok"},
		"records_path": "items",
		"pagination":   map[string]any{"type": "cursor", "field": "next"},
	}, "", nil, nil); err != nil {
		t.Fatalf("Configure: %v", err)
	}
	if h.Describe().Role != contract.RoleSource {
		t.Fatalf("http must be a source")
	}

	// buffered enough to hold both pages without a concurrent reader
	out := make(chan contract.Batch, len(pages))
	if err := h.Process(context.Background(), nil, out); err != nil {
		t.Fatalf("Process: %v", err)
	}
	close(out)

	if requests != len(pages) {
		t.Fatalf("expected %d requests (one per page), got %d", len(pages), requests)
	}
	var got []float64
	for batch := range out {
		for _, rec := range batch {
			got = append(got, rec["id"].(float64))
		}
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 records across both pages, got %v", got)
	}
}
