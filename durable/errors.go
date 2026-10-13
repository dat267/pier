package durable

import "fmt"

// Port of errors.ts: the durable errors a Session surfaces.

// StorageRejectedError reports a rejected storage request. A rejected read fails
// only that call; a rejected commit still fails the Session because its atomic
// outcome is not assumed safe.
type StorageRejectedError struct {
	Message string
	Cause   error
}

func (e *StorageRejectedError) Error() string { return e.Message }
func (e *StorageRejectedError) Unwrap() error { return e.Cause }

// SessionFailedError reports a Session that can no longer process work after a
// Storage failure. Cause is the first failure; reopen the Session to recover.
type SessionFailedError struct {
	Cause error
}

func (e *SessionFailedError) Error() string {
	return fmt.Sprintf("Session failed after a storage error; close and reopen it (cause: %v)", e.Cause)
}

func (e *SessionFailedError) Unwrap() error { return e.Cause }

// ReadAfterWriteError reports a transaction that read a table after its first
// table write (upstream ReadAfterWrite). Read every required row before
// writing.
type ReadAfterWriteError struct {
	Method string
}

func (e *ReadAfterWriteError) Error() string {
	return fmt.Sprintf("Tx.%s() cannot read tables after the first table write", e.Method)
}

// ConversationBusyError reports a submission that reached a busy conversation
// and was not admitted (upstream ConversationBusy).
type ConversationBusyError struct {
	ConversationID Id
}

func (e *ConversationBusyError) Error() string {
	return fmt.Sprintf("Conversation %d is busy", e.ConversationID)
}
