package httpengine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/datasplice-labs/datasplice-core/internal/manifest"
	"github.com/datasplice-labs/datasplice-core/internal/record"
)

// loadManifest writes yaml to a temp file and loads it, so tests go
// through the same strict parsing as real manifests.
func loadManifest(t *testing.T, yaml string) *manifest.Manifest {
	t.Helper()
	path := filepath.Join(t.TempDir(), "datasplice.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}

	m, err := manifest.Load(path)
	if err != nil {
		t.Fatalf("manifest.Load: %v", err)
	}

	return m
}

// simple builds a one-action manifest pointing at base. extra is
// appended verbatim to the manifest (top level), action is spliced under
// `actions.list` after role/method/path.
func simple(t *testing.T, base, extra, action string) *manifest.Manifest {
	t.Helper()
	return loadManifest(t, fmt.Sprintf(`
name: t
version: "0.1.0"
manifest_version: 1
base_url: %q
%s
actions:
  list:
    role: source
    method: GET
    path: /items
%s
`, base, extra, action))
}

// collect runs the engine and gathers everything it emits.
func collect(t *testing.T, e *Engine) ([]record.Record, error) {
	t.Helper()
	var got []record.Record
	err := e.Run(context.Background(), func(b []record.Record) error {
		got = append(got, b...)
		return nil
	})

	return got, err
}

func newEngine(t *testing.T, m *manifest.Manifest, secrets map[string]string, opts Options) *Engine {
	t.Helper()
	e, err := New(m, "list", map[string]any{}, secrets, opts)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	return e
}

// intParam reads an integer query parameter, or def when absent.
func intParam(r *http.Request, name string, def int) int {
	if n, err := strconv.Atoi(r.URL.Query().Get(name)); err == nil {
		return n
	}

	return def
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// T5.1: the spec's Zendesk example produces the expected URL.
func TestBuildsZendeskURL(t *testing.T) {
	m, err := manifest.Load("../manifest/testdata/valid/zendesk.yaml")
	if err != nil {
		t.Fatal(err)
	}

	settings := map[string]any{"subdomain": "acme", "resource": "tickets"}
	secrets := map[string]string{"ZENDESK_EMAIL": "me@acme.com", "ZENDESK_TOKEN": "tok"}
	e, err := New(m, "show_many", settings, secrets, Options{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	u := e.first
	if u.Host != "acme.zendesk.com" || u.Path != "/api/v2/tickets.json" || u.Query().Get("page[size]") != "100" {
		t.Fatalf("first URL = %s", u)
	}

	if e.auth.username != "me@acme.com/token" || e.auth.password != "tok" {
		t.Fatalf("auth = %+v", e.auth)
	}
}

// T5.2-5.5: each auth type sets the request up correctly.
func TestAuthTypes(t *testing.T) {
	cases := []struct {
		name, auth string
		check      func(t *testing.T, r *http.Request)
	}{
		{"bearer", "auth: {type: bearer, token: \"{{ secrets.T }}\"}", func(t *testing.T, r *http.Request) {
			if got := r.Header.Get("Authorization"); got != "Bearer s3cret" {
				t.Errorf("Authorization = %q", got)
			}
		}},
		{"basic email/token", "auth: {type: basic, username: \"{{ secrets.E }}/token\", password: \"{{ secrets.T }}\"}", func(t *testing.T, r *http.Request) {
			u, p, ok := r.BasicAuth()
			if !ok || u != "me@x.com/token" || p != "s3cret" {
				t.Errorf("basic auth = %q %q %v", u, p, ok)
			}
		}},
		{"header", "auth: {type: header, name: X-API-Key, value: \"{{ secrets.T }}\"}", func(t *testing.T, r *http.Request) {
			if got := r.Header.Get("X-API-Key"); got != "s3cret" {
				t.Errorf("X-API-Key = %q", got)
			}
		}},
		{"query", "auth: {type: query, name: api_key, value: \"{{ secrets.T }}\"}", func(t *testing.T, r *http.Request) {
			if got := r.URL.Query().Get("api_key"); got != "s3cret" {
				t.Errorf("api_key = %q", got)
			}
		}},
		{"none", "auth: {type: none}", func(t *testing.T, r *http.Request) {
			if r.Header.Get("Authorization") != "" || r.URL.RawQuery != "" {
				t.Errorf("expected no auth, got %v %q", r.Header, r.URL.RawQuery)
			}
		}},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var seen atomic.Bool
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				seen.Store(true)
				c.check(t, r)
				writeJSON(w, []any{})
			}))
			defer srv.Close()

			secrets := "secrets:\n  T: {}\n  E: {}"
			m := simple(t, srv.URL, secrets+"\n"+c.auth, "")
			e := newEngine(t, m, map[string]string{"T": "s3cret", "E": "me@x.com"}, Options{})
			if _, err := collect(t, e); err != nil {
				t.Fatalf("Run: %v", err)
			}

			if !seen.Load() {
				t.Fatal("server never saw a request")
			}
		})
	}
}

func TestMissingSecretFailsInNew(t *testing.T) {
	m := simple(t, "http://localhost", "secrets:\n  T: {}\nauth: {type: bearer, token: \"{{ secrets.T }}\"}", "")
	if _, err := New(m, "list", nil, map[string]string{}, Options{}); err == nil || !strings.Contains(err.Error(), "T") {
		t.Fatalf("New err = %v, want one naming the missing secret", err)
	}
}

// T5.7: the records path must be an array; errors say what was found.
func TestExtractErrors(t *testing.T) {
	cases := []struct {
		name, action, body, want string
	}{
		{"not an array", "    records: $.items", `{"items": {"a": 1}}`, "is an object, want an array"},
		{"matched nothing", "    records: $.items", `{"other": []}`, "matched nothing"},
		{"item not object", "    records: $.items", `{"items": [1]}`, "want an object"},
		{"root not array", "", `{"a": 1}`, "the response is an object"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte(c.body))
			}))
			defer srv.Close()

			_, err := collect(t, newEngine(t, simple(t, srv.URL, "", c.action), nil, Options{}))
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err = %v, want it to contain %q", err, c.want)
			}
		})
	}
}

// T5.8: a filtered path produces a derived field.
func TestDerivedFields(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"items": []any{
			map[string]any{"id": 1, "custom_fields": []any{map[string]any{"id": 360001, "value": "billing"}}},
			map[string]any{"id": 2, "custom_fields": []any{}},
		}})
	}))
	defer srv.Close()

	action := `    records: $.items
    fields:
      reason: "$.custom_fields[?(@.id==360001)].value"`
	got, err := collect(t, newEngine(t, simple(t, srv.URL, "", action), nil, Options{}))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if len(got) != 2 || got[0]["reason"] != "billing" || got[1]["reason"] != nil {
		t.Fatalf("records = %v", got)
	}

	if _, ok := got[1]["reason"]; !ok {
		t.Fatal("a missing derived field should be present as null, not absent")
	}
}

// T5.10: a 3-page server yields every record via cursor pagination.
func TestCursorPagination(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page := intParam(r, "p", 1)
		body := map[string]any{"items": []any{map[string]any{"n": page}}, "meta": map[string]any{"has_more": page < 3}}
		if page < 3 {
			body["links"] = map[string]any{"next": fmt.Sprintf("%s/items?p=%d", srv.URL, page+1)}
		}

		writeJSON(w, body)
	}))
	defer srv.Close()

	action := `    records: $.items
    paginate:
      style: cursor
      next: $.links.next
      until: $.meta.has_more == false`
	got, err := collect(t, newEngine(t, simple(t, srv.URL, "", action), nil, Options{}))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if len(got) != 3 {
		t.Fatalf("got %d records, want 3: %v", len(got), got)
	}
}

func TestCursorRefusesOtherHost(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"items": []any{}, "next": "http://evil.example.com/steal"})
	}))
	defer srv.Close()

	action := "    records: $.items\n    paginate:\n      style: cursor\n      next: $.next"
	_, err := collect(t, newEngine(t, simple(t, srv.URL, "", action), nil, Options{}))
	if err == nil || !strings.Contains(err.Error(), "different host") {
		t.Fatalf("err = %v, want a different-host refusal", err)
	}
}

func TestCursorMustAdvance(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"items": []any{}, "next": srv.URL + r.URL.RequestURI()})
	}))
	defer srv.Close()

	action := "    records: $.items\n    paginate:\n      style: cursor\n      next: $.next"
	_, err := collect(t, newEngine(t, simple(t, srv.URL, "", action), nil, Options{}))
	if err == nil || !strings.Contains(err.Error(), "just fetched") {
		t.Fatalf("err = %v, want a no-progress error", err)
	}
}

// T5.11: page style increments the param until an empty result.
func TestPagePagination(t *testing.T) {
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		page := intParam(r, "page", 0)
		items := []any{}
		if page <= 2 {
			items = append(items, map[string]any{"page": page})
		}

		writeJSON(w, map[string]any{"items": items})
	}))
	defer srv.Close()

	action := "    records: $.items\n    paginate:\n      style: page\n      param: page\n      start: 1\n      until: empty"
	got, err := collect(t, newEngine(t, simple(t, srv.URL, "", action), nil, Options{}))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if len(got) != 2 || requests.Load() != 3 {
		t.Fatalf("got %d records over %d requests, want 2 over 3", len(got), requests.Load())
	}
}

// T5.12: the comparison form also ends page-style pagination.
func TestPageUntilComparison(t *testing.T) {
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := requests.Add(1)
		writeJSON(w, map[string]any{"items": []any{map[string]any{"n": n}}, "done": n == 2})
	}))
	defer srv.Close()

	action := "    records: $.items\n    paginate:\n      style: page\n      param: page\n      until: $.done == true"
	got, err := collect(t, newEngine(t, simple(t, srv.URL, "", action), nil, Options{}))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if len(got) != 2 || requests.Load() != 2 {
		t.Fatalf("got %d records over %d requests, want 2 over 2", len(got), requests.Load())
	}
}

// T5.18: max_records stops mid-page and makes no further requests.
func TestMaxRecordsStopsMidPage(t *testing.T) {
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		items := make([]any, 5)
		for i := range items {
			items[i] = map[string]any{"i": i}
		}

		writeJSON(w, map[string]any{"items": items})
	}))
	defer srv.Close()

	action := "    records: $.items\n    paginate:\n      style: page\n      param: page\n      until: empty"
	got, err := collect(t, newEngine(t, simple(t, srv.URL, "", action), nil, Options{MaxRecords: 7}))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if len(got) != 7 || requests.Load() != 2 {
		t.Fatalf("got %d records over %d requests, want 7 over 2", len(got), requests.Load())
	}
}

// T5.13: requests are spaced as the rate limit says.
func TestRateLimitSpacesRequests(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page := intParam(r, "page", 0)
		items := []any{}
		if page < 4 {
			items = append(items, map[string]any{"page": page})
		}

		writeJSON(w, map[string]any{"items": items})
	}))
	defer srv.Close()

	action := "    records: $.items\n    paginate:\n      style: page\n      param: page\n      until: empty"
	m := simple(t, srv.URL, "rate_limit: {requests: 25, per: second}", action)

	start := time.Now()
	if _, err := collect(t, newEngine(t, m, nil, Options{})); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// 4 requests at 25/s (40ms apart): at least 3 gaps.
	if elapsed := time.Since(start); elapsed < 110*time.Millisecond {
		t.Fatalf("4 requests took %v, want them spaced at least ~120ms", elapsed)
	}
}

func TestRateLimitPerActionOverride(t *testing.T) {
	m := simple(t, "http://localhost", "rate_limit:\n  requests: 1\n  per: hour\n  overrides:\n    list: {requests: 100, per: second}", "")
	if e := newEngine(t, m, nil, Options{}); e.limiter.Limit() < 50 {
		t.Fatalf("limit = %v, want the override (100/s)", e.limiter.Limit())
	}
}

// fakeWait records delays instead of sleeping.
type fakeWait struct{ delays []time.Duration }

func (f *fakeWait) wait(_ context.Context, d time.Duration) error {
	f.delays = append(f.delays, d)
	return nil
}

// T5.14: a 429 with Retry-After is retried after that long.
func TestRetryHonoursRetryAfter(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}

		writeJSON(w, []any{map[string]any{"ok": true}})
	}))
	defer srv.Close()

	fw := &fakeWait{}
	m := simple(t, srv.URL, "retry: {on: [429], respect_retry_after: true, max_attempts: 3}", "")
	got, err := collect(t, newEngine(t, m, nil, Options{Wait: fw.wait}))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if len(got) != 1 || len(fw.delays) != 1 || fw.delays[0] != time.Second {
		t.Fatalf("records = %v, delays = %v; want 1 record after one 1s wait", got, fw.delays)
	}
}

// T5.15: without Retry-After, delays grow and carry jitter.
func TestRetryBackoffGrowsWithJitter(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) <= 3 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		writeJSON(w, []any{})
	}))
	defer srv.Close()

	fw := &fakeWait{}
	m := simple(t, srv.URL, "retry: {on: [500], max_attempts: 4, backoff: exponential, initial_delay: 100ms}", "")
	if _, err := collect(t, newEngine(t, m, nil, Options{Wait: fw.wait})); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Attempt n waits in [base/2, base] where base = 100ms * 2^(n-1).
	if len(fw.delays) != 3 {
		t.Fatalf("delays = %v, want 3", fw.delays)
	}

	for i, d := range fw.delays {
		base := 100 * time.Millisecond << i
		if d < base/2 || d > base {
			t.Errorf("delay %d = %v, want within [%v, %v]", i+1, d, base/2, base)
		}
	}
}

func TestBackoffIsNotConstant(t *testing.T) {
	p := newRetryPolicy(&manifest.Retry{On: []int{500}, Backoff: "exponential", InitialDelay: "1s"})
	seen := map[time.Duration]bool{}
	for range 20 {
		seen[p.delay(3, http.Header{}, time.Now())] = true
	}

	if len(seen) < 2 {
		t.Fatal("20 identical delays: jitter isn't applied")
	}
}

func TestParseRetryAfterDate(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	got, ok := parseRetryAfter(now.Add(90*time.Second).Format(http.TimeFormat), now)
	if !ok || got != 90*time.Second {
		t.Fatalf("parseRetryAfter = %v, %v", got, ok)
	}
}

// T5.16: giving up names the URL, status, and records already emitted.
func TestRetryGivesUpWithContext(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("p") == "2" {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}

		writeJSON(w, map[string]any{
			"items": []any{map[string]any{"a": 1}, map[string]any{"a": 2}},
			"next":  srv.URL + "/items?p=2",
		})
	}))
	defer srv.Close()

	fw := &fakeWait{}
	action := `    records: $.items
    paginate:
      style: cursor
      next: $.next`
	m := simple(t, srv.URL, "retry: {on: [503], max_attempts: 3, initial_delay: 1ms}", action)
	got, err := collect(t, newEngine(t, m, nil, Options{Wait: fw.wait}))

	var re *RequestError
	if !errors.As(err, &re) {
		t.Fatalf("err = %v, want a *RequestError", err)
	}

	if len(got) != 2 || re.Status != 503 || re.Attempts != 3 || re.Emitted != 2 || !strings.Contains(re.URL, "p=2") {
		t.Fatalf("records = %d, err = %+v", len(got), re)
	}

	if len(fw.delays) != 2 {
		t.Fatalf("waited %d times, want 2 (between 3 attempts)", len(fw.delays))
	}
}

func TestNonRetryableStatusFailsImmediately(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("no such thing"))
	}))
	defer srv.Close()

	fw := &fakeWait{}
	m := simple(t, srv.URL, "retry: {on: [500], max_attempts: 5}", "")
	_, err := collect(t, newEngine(t, m, nil, Options{Wait: fw.wait}))

	var re *RequestError
	if !errors.As(err, &re) || re.Status != 404 || re.Attempts != 1 || len(fw.delays) != 0 {
		t.Fatalf("err = %v, delays = %v; want an immediate 404", err, fw.delays)
	}

	if !strings.Contains(err.Error(), "no such thing") {
		t.Fatalf("error %q should include the response body", err)
	}
}

// T5.17: Ctrl-C during a backoff wait returns promptly.
func TestCancelDuringRetryWait(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "30")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	m := simple(t, srv.URL, "retry: {on: [429], respect_retry_after: true, max_attempts: 3}", "")
	e := newEngine(t, m, nil, Options{})

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	err := e.Run(ctx, func([]record.Record) error { return nil })
	if err != context.Canceled {
		t.Fatalf("err = %v, want context.Canceled", err)
	}

	if time.Since(start) > time.Second {
		t.Fatalf("took %v to notice cancellation", time.Since(start))
	}
}

func TestCancelDuringRateLimitWait(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, []any{})
	}))
	defer srv.Close()

	// One request per hour: the second one would wait essentially forever.
	action := "    paginate:\n      style: page\n      param: page\n      until: $.never == true"
	m := simple(t, srv.URL, "rate_limit: {requests: 1, per: hour}", action)
	e := newEngine(t, m, nil, Options{})

	// Cancel (rather than a deadline): the limiter fails fast on a
	// deadline it can't meet, but here it has to be interrupted mid-wait.
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	if err := e.Run(ctx, func([]record.Record) error { return nil }); err != context.Canceled {
		t.Fatalf("err = %v, want context.Canceled", err)
	}

	if time.Since(start) > time.Second {
		t.Fatalf("took %v to notice cancellation", time.Since(start))
	}
}

// Query-string auth must never appear in an error.
func TestQueryAuthNotLeakedInErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))

	extra := "secrets:\n  K: {}\nauth: {type: query, name: api_key, value: \"{{ secrets.K }}\"}"
	e := newEngine(t, simple(t, srv.URL, extra, ""), map[string]string{"K": "TOPSECRET"}, Options{})

	_, err := collect(t, e)
	if err == nil || strings.Contains(err.Error(), "TOPSECRET") {
		t.Fatalf("500 error = %v; must not contain the key", err)
	}

	// Transport failure: the server is gone.
	srv.Close()
	_, err = collect(t, e)
	if err == nil || strings.Contains(err.Error(), "TOPSECRET") {
		t.Fatalf("connection error = %v; must not contain the key", err)
	}
}

func TestRedirectToOtherHostRefused(t *testing.T) {
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("the redirect target must never be contacted")
	}))
	defer other.Close()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL, http.StatusFound)
	}))
	defer srv.Close()

	// 127.0.0.1:port differs by port, which counts as a different host.
	if _, err := collect(t, newEngine(t, simple(t, srv.URL, "", ""), nil, Options{})); err == nil {
		t.Fatal("expected the cross-host redirect to be refused")
	}
}
