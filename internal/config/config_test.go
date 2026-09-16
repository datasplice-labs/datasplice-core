package config

import (
	"os"
	"path/filepath"
	"testing"
)

func writeMain(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "main.yaml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadMainConfigDefaults(t *testing.T) {
	path := writeMain(t, `
name: test
steps:
  - uses: datasplice/csv
`)
	m, err := LoadMain(path)
	if err != nil {
		t.Fatalf("LoadMain: %v", err)
	}
	if m.Config.OnError != OnErrorFail || m.Config.Mode != ModeRecord || m.Config.LogLevel != "info" {
		t.Fatalf("defaults = %+v", m.Config)
	}
}

func TestLoadMainConfigValidation(t *testing.T) {
	cases := []struct {
		name string
		yaml string
	}{
		{"bad on_error", "on_error: acid"},
		{"skip_record not implemented", "on_error: skip_record"},
		{"bad mode", "mode: turbo"},
		{"bulk not implemented", "mode: bulk"},
		{"bad log_level", "log_level: verbose"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			path := writeMain(t, "name: test\nconfig:\n  "+c.yaml+"\nsteps:\n  - uses: datasplice/csv\n")
			if _, err := LoadMain(path); err == nil {
				t.Fatalf("expected error for %s", c.name)
			}
		})
	}
}

func TestLoadMainConfigUnknownKeyRejected(t *testing.T) {
	path := writeMain(t, `
name: test
config:
  bulk_size: 100
steps:
  - uses: datasplice/csv
`)
	if _, err := LoadMain(path); err == nil {
		t.Fatalf("expected error for unknown config key bulk_size")
	}
}
