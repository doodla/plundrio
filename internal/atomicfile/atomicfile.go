// Package atomicfile provides crash-safe file replacement: write to a uniquely
// named temp file in the same directory, then rename over the target. The rename
// is atomic on POSIX, so a crash or full disk mid-write leaves either the old
// complete file or the new one — never a truncated file.
//
// It is the single implementation behind the daemon's small JSON state files
// (the category map and the runtime-overrides file), which previously each
// carried their own copy of the temp+rename dance.
package atomicfile

import (
	"fmt"
	"os"
	"path/filepath"
)

// WriteFile atomically writes data to path with the given permission bits.
//
// A uniquely named temp file (os.CreateTemp, not a fixed path+".tmp") lets two
// concurrent writers each rename their own temp over the target without
// clobbering a half-written shared temp — last writer wins, no corruption.
// os.CreateTemp creates the temp 0600, so perm is applied via Chmod before the
// rename. On any failure before the rename the temp file is removed; after a
// successful rename that cleanup is a harmless no-op.
func WriteFile(path string, data []byte, perm os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp file: %w", err)
	}
	if err := os.Chmod(tmpName, perm); err != nil {
		return fmt.Errorf("chmod temp file: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("rename temp file into place: %w", err)
	}
	return nil
}
