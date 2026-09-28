package registry

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGetFetchesAndCaches(t *testing.T) {
	g := &fakeGitHub{
		repos:     map[string][]string{"myorg/pkg": {"v0.2.0"}},
		manifests: map[string][]byte{"myorg/pkg@v0.2.0": []byte("name: pkg\n")},
	}
	f := newFetcher(t, g)
	home := t.TempDir()
	ref := Ref{Host: "github.com", Path: "myorg/pkg", Version: "v0.2.0"}

	hash, err := Get(context.Background(), f, home, ref)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if hash != hashOf([]byte("name: pkg\n")) {
		t.Fatalf("hash = %q", hash)
	}

	data, err := os.ReadFile(CachePath(home, ref))
	if err != nil {
		t.Fatalf("cached file missing: %v", err)
	}
	if string(data) != "name: pkg\n" {
		t.Fatalf("cached data = %q", data)
	}
}

// T6.2: a second get doesn't re-download.
func TestGetSkipsNetworkWhenAlreadyCached(t *testing.T) {
	home := t.TempDir()
	ref := Ref{Host: "github.com", Path: "myorg/pkg", Version: "v0.2.0"}
	if err := os.MkdirAll(filepath.Dir(CachePath(home, ref)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(CachePath(home, ref), []byte("cached already\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// A Fetcher with no server behind it: any network call panics/errors.
	f := &Fetcher{APIBase: "http://127.0.0.1:1", RawBase: "http://127.0.0.1:1"}

	hash, err := Get(context.Background(), f, home, ref)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if hash != hashOf([]byte("cached already\n")) {
		t.Fatalf("hash = %q, want the hash of the already-cached file", hash)
	}
}

func TestLoadVerifiesAgainstLockfile(t *testing.T) {
	home := t.TempDir()
	ref := Ref{Host: "github.com", Path: "myorg/pkg", Version: "v0.2.0"}
	if err := os.MkdirAll(filepath.Dir(CachePath(home, ref)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(CachePath(home, ref), []byte("name: pkg\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	lock := &Lockfile{Entries: map[Ref]string{ref: hashOf([]byte("name: pkg\n"))}}

	data, err := Load(home, lock, ref)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if string(data) != "name: pkg\n" {
		t.Fatalf("data = %q", data)
	}
}

// T6.8: a ref missing from the lockfile fails with an actionable message.
func TestLoadFailsWhenNotLocked(t *testing.T) {
	lock := &Lockfile{Entries: map[Ref]string{}}
	ref := Ref{Host: "github.com", Path: "myorg/pkg", Version: "v0.2.0"}

	_, err := Load(t.TempDir(), lock, ref)
	if err == nil || !strings.Contains(err.Error(), "datasplice get") {
		t.Fatalf("err = %v, want it to suggest `datasplice get`", err)
	}
}

func TestLoadFailsWhenNotCached(t *testing.T) {
	ref := Ref{Host: "github.com", Path: "myorg/pkg", Version: "v0.2.0"}
	lock := &Lockfile{Entries: map[Ref]string{ref: "deadbeef"}}

	_, err := Load(t.TempDir(), lock, ref)
	if err == nil || !strings.Contains(err.Error(), "datasplice get") {
		t.Fatalf("err = %v, want it to suggest `datasplice get`", err)
	}
}

// T6.5: editing a cached manifest causes a clear failure.
func TestLoadFailsOnHashMismatch(t *testing.T) {
	home := t.TempDir()
	ref := Ref{Host: "github.com", Path: "myorg/pkg", Version: "v0.2.0"}
	if err := os.MkdirAll(filepath.Dir(CachePath(home, ref)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(CachePath(home, ref), []byte("tampered\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	lock := &Lockfile{Entries: map[Ref]string{ref: hashOf([]byte("name: pkg\n"))}}

	_, err := Load(home, lock, ref)
	if err == nil || !strings.Contains(err.Error(), "does not match datasplice.lock") {
		t.Fatalf("err = %v, want a hash-mismatch error", err)
	}
}

func TestHomeDefaultAndOverride(t *testing.T) {
	t.Setenv("DATASPLICE_PATH", "")
	home, err := Home()
	if err != nil {
		t.Fatalf("Home: %v", err)
	}
	if !strings.HasSuffix(home, ".datasplice") {
		t.Fatalf("Home() = %q, want it to end in .datasplice", home)
	}

	t.Setenv("DATASPLICE_PATH", "/custom/path")
	home, err = Home()
	if err != nil {
		t.Fatalf("Home: %v", err)
	}
	if home != "/custom/path" {
		t.Fatalf("Home() = %q, want the DATASPLICE_PATH override", home)
	}
}
