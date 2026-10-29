package coding

import (
	"errors"
	"io"
	"os"
)

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
