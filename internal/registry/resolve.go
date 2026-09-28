package registry

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

// Get resolves ref for `datasplice get`: if it's already cached, that's
// trusted as-is (published tags are never supposed to move) and nothing is fetched;
// otherwise it's fetched and cached.
// Either way the returned hash is what goes in datasplice.lock.
func Get(ctx context.Context, f *Fetcher, home string, ref Ref) (hash string, err error) {
	path := CachePath(home, ref)

	if data, err := os.ReadFile(path); err == nil {
		return hashOf(data), nil
	}

	data, err := f.Fetch(ctx, ref)
	if err != nil {
		return "", err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", fmt.Errorf("caching %s: %w", ref, err)
	}

	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", fmt.Errorf("caching %s: %w", ref, err)
	}

	return hashOf(data), nil
}

// Load reads ref's manifest for validate/plan/run — no network, ever.
// Fails clearly if ref was never `get`-ed, isn't cached, or the cached
// bytes no longer match the pinned hash (edited by hand, or a tag moved,
// which is the one thing publishing a package must never do).
func Load(home string, lock *Lockfile, ref Ref) ([]byte, error) {
	wantHash, ok := lock.Hash(ref)
	if !ok {
		return nil, fmt.Errorf("%s: not in datasplice.lock — run `datasplice get`", ref)
	}

	path := CachePath(home, ref)

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%s: not cached — run `datasplice get`", ref)
		}

		return nil, fmt.Errorf("%s: %w", ref, err)
	}

	if got := hashOf(data); got != wantHash {
		return nil, fmt.Errorf("%s: cached manifest does not match datasplice.lock (got sha256:%s, want sha256:%s) — run `datasplice get`", ref, got, wantHash)
	}

	return data, nil
}
