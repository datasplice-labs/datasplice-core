package registry

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
)

// Home is the cache root, ~/.datasplice by default — DATASPLICE_PATH
// overrides it (Lambda's /tmp, a read-only container, etc).
func Home() (string, error) {
	if h := os.Getenv("DATASPLICE_PATH"); h != "" {
		return h, nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("finding home directory: %w", err)
	}

	return filepath.Join(home, ".datasplice"), nil
}

// CachePath is where ref's manifest lives once fetched:
// <home>/packages/<host>/<path>/<version>/datasplice.yaml
func CachePath(home string, ref Ref) string {
	return filepath.Join(home, "packages", ref.Host, filepath.FromSlash(ref.Path), ref.Version, "datasplice.yaml")
}

func hashOf(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
