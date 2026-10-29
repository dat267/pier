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
	if err := os.MkdirAll(path, 0o700); err != nil {
		return err
	}
	return os.Chmod(path, 0o700)
}

// writePrivateFile creates or replaces private user data with owner-only mode.
func writePrivateFile(path string, data []byte) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if err := file.Chmod(0o600); err != nil {
		return errors.Join(err, file.Close())
	}
	written, writeErr := file.Write(data)
	if writeErr == nil && written != len(data) {
		writeErr = io.ErrShortWrite
	}
	return errors.Join(writeErr, file.Close())
}
