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
		if err := json.NewEncoder(w).Encode(body); err != nil {
			t.Errorf("encoding response: %v", err)
		}
	}))

	defer srv.Close()

	h := NewHTTP()
	if err := h.Configure(map[string]any{
		"url":     srv.URL,
		"auth":    map[string]any{"type": "bearer", "token": "tok"},
		"records": "$.items",
		"paginate": map[string]any{
			"style": "cursor",
			"next":  "$.next",
		},
	}, nil); err != nil {
		t.Fatalf("Configure: %v", err)
	}
	if !h.Describe().HasRole(contract.RoleSource) {
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

func TestHTTPRequiresURL(t *testing.T) {
	h := NewHTTP()
	if err := h.Configure(map[string]any{}, nil); err == nil {
		t.Fatal("expected an error: with.url is required")
	}
}

func TestHTTPRejectsBadURL(t *testing.T) {
	h := NewHTTP()
	if err := h.Configure(map[string]any{"url": "not-a-url"}, nil); err == nil {
		t.Fatal("expected an error: with.url must be absolute")
	}
}

// A bare host with no path (e.g. an httptest server URL) must still
// pass manifest validation, which requires a non-empty action path.
func TestHTTPBareHostDefaultsToRootPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			t.Errorf("path = %q, want /", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode([]any{})
	}))
	defer srv.Close()

	h := NewHTTP()
	if err := h.Configure(map[string]any{"url": srv.URL}, nil); err != nil {
		t.Fatalf("Configure: %v", err)
	}

	out := make(chan contract.Batch, 1)
	if err := h.Process(context.Background(), nil, out); err != nil {
		t.Fatalf("Process: %v", err)
	}
}

// query params already in with.url survive alongside with.query.
func TestHTTPMergesURLAndWithQuery(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("from_url") != "1" || r.URL.Query().Get("extra") != "2" {
			t.Errorf("query = %q", r.URL.RawQuery)
		}
		_ = json.NewEncoder(w).Encode([]any{})
	}))
	defer srv.Close()

	h := NewHTTP()
	if err := h.Configure(map[string]any{
		"url":   srv.URL + "/items?from_url=1",
		"query": map[string]any{"extra": "2"},
	}, nil); err != nil {
		t.Fatalf("Configure: %v", err)
	}

	out := make(chan contract.Batch, 1)
	if err := h.Process(context.Background(), nil, out); err != nil {
		t.Fatalf("Process: %v", err)
	}
}
