package coding

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// ensurePrivateDirectory creates or hardens a directory for global agent data.
func ensurePrivateDirectory(path string) error {
	clean := filepath.Clean(path)
	if clean == "." || clean == ".." || (filepath.IsAbs(clean) && filepath.Dir(clean) == clean) {
		return fmt.Errorf("refusing to restrict permissions on %q", path)
	}
	if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("refusing to restrict symlink %q", path)
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.MkdirAll(path, 0o700); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("refusing to restrict non-directory %q", path)
	}
	return os.Chmod(path, 0o700)
}

// writePrivateFile creates or replaces private user data with owner-only mode.
func writePrivateFile(path string, data []byte) error {
	return replaceFileAtomically(path, data)
}

// WritePrivateFile creates or replaces private user data with owner-only mode.
// It is exported for the interactive package's export writer, which had its own
// copy of the same logic.
func WritePrivateFile(path string, data []byte) error { return writePrivateFile(path, data) }

// replaceFileAtomically writes data to a new file beside path, flushes it, and
// renames it over the target, so a failed or interrupted write leaves the
// previous content readable (A506). Upstream truncates the target in place
// (`writeFileSync`), which is why a full disk during a session rewrite loses the
// transcript; see D454.
//
// A directory that forbids creating the temp file fails the write rather than
// falling back to a destructive in-place overwrite: the caller can retry once
// there is room, but the previous content cannot be recovered.
func replaceFileAtomically(path string, data []byte) error {
	temp, err := os.CreateTemp(filepath.Dir(path), ".pi-replace-*.tmp")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	written, writeErr := temp.Write(data)
	if writeErr == nil && written != len(data) {
		writeErr = io.ErrShortWrite
	}
	if writeErr == nil {
		// Without this a power loss can publish the rename while the data is
		// still only in the page cache, leaving an empty file.
		writeErr = temp.Sync()
	}
	if closeErr := temp.Close(); writeErr == nil {
		writeErr = closeErr
	}
	if writeErr != nil {
		_ = os.Remove(tempPath)
		return writeErr
	}
	if err := os.Rename(tempPath, path); err != nil {
		_ = os.Remove(tempPath)
		return err
	}
	return nil
}
