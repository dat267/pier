package coding

import (
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"
)

// SessionWriteError identifies a failed session persistence operation.
// D210: upstream core/session-manager.ts throws synchronously; the Go port
// records queued failures without invoking user callbacks under manager locks.
type SessionWriteError struct {
	Path      string
	Operation string
	Error     error
}

func (e SessionWriteError) String() string {
	return fmt.Sprintf("Session %s failed (%s): %v", e.Operation, e.Path, e.Error)
}

func (m *SessionManager) recordWriteResult(path, operation string, err error) {
	m.writeErrorsMu.Lock()
	defer m.writeErrorsMu.Unlock()
	if err == nil {
		delete(m.writeFailures, path)
		return
	}
	if m.writeFailures == nil {
		m.writeFailures = make(map[string]error)
	}
	m.writeFailures[path] = fmt.Errorf("session %s failed (%s): %w", operation, path, err)
	m.writeErrors = append(m.writeErrors, SessionWriteError{Path: path, Operation: operation, Error: err})
	m.notifyWriteErrors()
}

// WriteErrorsReady is a coalesced wakeup for consumers of DrainWriteErrors.
// Sending never blocks a save; diagnostics remain lossless in the error buffer.
func (m *SessionManager) WriteErrorsReady() <-chan struct{} {
	m.writeErrorsMu.Lock()
	defer m.writeErrorsMu.Unlock()
	if m.writeErrorsReady == nil {
		m.writeErrorsReady = make(chan struct{}, 1)
	}
	if len(m.writeErrors) > 0 {
		m.notifyWriteErrors()
	}
	return m.writeErrorsReady
}

// notifyWriteErrors is called only with writeErrorsMu held.
func (m *SessionManager) notifyWriteErrors() {
	select {
	case m.writeErrorsReady <- struct{}{}:
	default:
	}
}

// DrainWriteErrors returns and clears recorded failures. It never waits for
// disk I/O. Draining diagnostics does not clear unresolved FlushWrites failures.
func (m *SessionManager) DrainWriteErrors() []SessionWriteError {
	m.writeErrorsMu.Lock()
	defer m.writeErrorsMu.Unlock()
	errors := m.writeErrors
	m.writeErrors = nil
	select {
	case <-m.writeErrorsReady:
	default:
	}
	return errors
}

// FlushWrites waits for accepted writes, then reports unresolved failures.
// Completion of the queue alone does not imply successful persistence.
func (m *SessionManager) FlushWrites() error {
	if m.writeQueue != nil {
		m.writeQueue.Flush()
	}
	m.writeErrorsMu.Lock()
	defer m.writeErrorsMu.Unlock()
	var failures []error
	for _, err := range m.writeFailures {
		failures = append(failures, err)
	}
	return errors.Join(failures...)
}

// sessionWriteState belongs to one session file generation. Only the FIFO
// worker (or the synchronous caller under SessionManager.mu) accesses it.
// A failed write requires the next save to rewrite its entire accepted prefix.
type sessionWriteState struct {
	initialized bool
	claimed     bool
}

func (s *sessionWriteState) rewrite(path string, entries []FileEntry) error {
	if s.initialized || s.claimed {
		err := writeSessionEntries(path, entries)
		s.initialized = err == nil
		return err
	}
	claimed, err := writeSessionEntriesExclusive(path, entries)
	s.claimed = claimed
	s.initialized = err == nil
	return err
}

func (s *sessionWriteState) append(path, line string, marshalErr error, entries []FileEntry) error {
	if !s.initialized {
		return s.rewrite(path, entries)
	}
	err := marshalErr
	if err == nil {
		err = writeSessionLine(path, line)
	}
	s.initialized = err == nil
	return err
}

func writeSessionEntries(path string, entries []FileEntry) error {
	data, err := marshalSessionEntries(entries)
	if err != nil {
		return err
	}
	return writePrivateSessionFile(path, data)
}

func writeSessionEntriesExclusive(path string, entries []FileEntry) (bool, error) {
	data, err := marshalSessionEntries(entries)
	if err != nil {
		return false, err
	}
	file, err := openPrivateSessionFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY)
	if err != nil {
		return false, err
	}
	_, writeErr := file.Write(data)
	closeErr := file.Close()
	return true, errors.Join(writeErr, closeErr)
}

func marshalSessionEntries(entries []FileEntry) ([]byte, error) {
	var buffer strings.Builder
	for _, entry := range entries {
		line, err := MarshalFileEntry(entry)
		if err != nil {
			return nil, err
		}
		buffer.WriteString(line)
	}
	return []byte(buffer.String()), nil
}

// writeSessionLine appends one pre-marshaled line, including close failures.
func writeSessionLine(path string, line string) error {
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, writeErr := file.WriteString(line)
	return errors.Join(writeErr, file.Close())
}

// EnsurePrivateSessionDir creates a session directory and restricts its POSIX
// permissions. It also hardens existing directories created by older releases.
func EnsurePrivateSessionDir(path string) error {
	if err := os.MkdirAll(path, 0o700); err != nil {
		return err
	}
	if runtime.GOOS == "windows" {
		return nil // Windows access is governed by inherited ACLs, not POSIX mode bits.
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.Mode().Perm() == 0o700 {
		return nil
	}
	return os.Chmod(path, 0o700)
}

func openPrivateSessionFile(path string, flags int) (*os.File, error) {
	file, err := os.OpenFile(path, flags, 0o600)
	if err != nil {
		return nil, err
	}
	if err := file.Chmod(0o600); err != nil {
		return nil, errors.Join(err, file.Close())
	}
	return file, nil
}

func writePrivateSessionFile(path string, data []byte) error {
	if err := os.Chmod(path, 0o600); err != nil && !os.IsNotExist(err) {
		return err
	}
	file, err := openPrivateSessionFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY)
	if err != nil {
		return err
	}
	written, writeErr := file.Write(data)
	if writeErr == nil && written != len(data) {
		writeErr = io.ErrShortWrite
	}
	return errors.Join(writeErr, file.Close())
}
