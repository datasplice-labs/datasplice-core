package redact

import (
	"bytes"
	"strings"
	"testing"
)

func TestWriterRedactsKnownSecrets(t *testing.T) {
	var buf bytes.Buffer
	w := New(&buf, map[string]string{"token": "sk-super-secret", "empty": ""})

	n, err := w.Write([]byte("Authorization: Bearer sk-super-secret\nother line\n"))
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if n != len("Authorization: Bearer sk-super-secret\nother line\n") {
		t.Fatalf("Write returned n=%d, want len(p)", n)
	}
	got := buf.String()
	if strings.Contains(got, "sk-super-secret") {
		t.Fatalf("secret leaked into output: %q", got)
	}
	if !strings.Contains(got, "***") {
		t.Fatalf("expected redaction marker in output: %q", got)
	}
	if !strings.Contains(got, "other line") {
		t.Fatalf("non-secret text was altered: %q", got)
	}
}

func TestStringLeavesNonSecretsAlone(t *testing.T) {
	w := New(nil, map[string]string{"token": "abc123"})
	got := w.String("no secrets here")
	if got != "no secrets here" {
		t.Fatalf("String altered non-secret text: %q", got)
	}
}
