package builtin

import (
	"fmt"
	"os"
	"path/filepath"
)

// atomicWriteFile writes data to a temp file in path's directory and
// renames it over path, so the write is either complete or path is
// untouched (docs/getting-started/file-structure.md "config": "the
// output file is either complete or absent"). For a sink that builds its
// whole output in memory before writing once (json); a sink that streams
// writes (csv) needs its own temp-file-then-rename around its writer.
func atomicWriteFile(path string, data []byte) (err error) {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".datasplice-tmp-*")
	if err != nil {
		return fmt.Errorf("atomic write: %w", err)
	}

	tmpPath := tmp.Name()
	defer func() {
		if err != nil {
			_ = os.Remove(tmpPath)
		}
	}()

	if _, err = tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("atomic write: %w", err)
	}

	if err = tmp.Close(); err != nil {
		return fmt.Errorf("atomic write: %w", err)
	}

	if err = os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("atomic write: %w", err)
	}

	return nil
}
