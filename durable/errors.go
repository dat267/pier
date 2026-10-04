package durable

import "fmt"

// Port of errors.ts: the durable errors a Session surfaces.

// StorageRejectedError reports a batch the storage rejected before any durable
// effect, so the owning Session may continue safely (upstream StorageRejected).
type StorageRejectedError struct {
	Message string
	Cause   error
}

func (e *StorageRejectedError) Error() string { return e.Message }
func (e *StorageRejectedError) Unwrap() error { return e.Cause }

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
