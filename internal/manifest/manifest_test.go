package manifest

import (
	"path/filepath"
	"testing"
)

// TestValidFixturesLoad covers T4.1: the Zendesk example (and others)
// from the spec unmarshal and validate cleanly.
func TestValidFixturesLoad(t *testing.T) {
	files, err := filepath.Glob("testdata/valid/*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("no valid fixtures found")
	}
	for _, f := range files {
		t.Run(f, func(t *testing.T) {
			if _, err := Load(f); err != nil {
				t.Fatalf("Load(%s): %v", f, err)
			}
		})
	}
}

// TestInvalidFixturesFail covers T4.11: at least 10 invalid cases, each
// rejected with a clear error.
func TestInvalidFixturesFail(t *testing.T) {
	files, err := filepath.Glob("testdata/invalid/*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) < 10 {
		t.Fatalf("only %d invalid fixtures, want at least 10", len(files))
	}
	for _, f := range files {
		t.Run(f, func(t *testing.T) {
			if _, err := Load(f); err == nil {
				t.Fatalf("Load(%s) succeeded, want an error", f)
			}
		})
	}
}

func TestZendeskFieldsUnmarshalCorrectly(t *testing.T) {
	m, err := Load("testdata/valid/zendesk.yaml")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if m.Name != "zendesk" || m.Version != "0.2.0" || m.ManifestVersion != 1 {
		t.Fatalf("top-level fields = %+v", m)
	}
	if !m.Settings["resource"].Required || m.Settings["resource"].OneOf[0] != "tickets" {
		t.Fatalf("settings.resource = %+v", m.Settings["resource"])
	}
	if _, ok := m.Secrets["ZENDESK_TOKEN"]; !ok {
		t.Fatalf("secrets.ZENDESK_TOKEN missing")
	}
	if m.Auth.Type != "basic" || m.Auth.Username == "" {
		t.Fatalf("auth = %+v", m.Auth)
	}
	if m.RateLimit.Requests != 200 || m.RateLimit.Overrides["show_many"].Requests != 10 {
		t.Fatalf("rate_limit = %+v", m.RateLimit)
	}
	if m.Retry.MaxAttempts != 5 || !m.Retry.RespectRetryAfter {
		t.Fatalf("retry = %+v", m.Retry)
	}

	src, ok := m.Actions["show_many"]
	if !ok || src.Role != "source" || src.Paginate.Style != "cursor" {
		t.Fatalf("actions.show_many = %+v", src)
	}
	tr, ok := m.Actions["context"]
	if !ok || tr.Role != "transform" || tr.Output["context"] != "$.content[0].text" {
		t.Fatalf("actions.context = %+v", tr)
	}
	sink, ok := m.Actions["create_many"]
	if !ok || sink.Role != "sink" {
		t.Fatalf("actions.create_many = %+v", sink)
	}
}
