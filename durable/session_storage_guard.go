package durable

import "context"

// sessionStorageGuard makes every Storage operation fail the owning Session on
// unexpected errors. StorageRejectedError represents an invalid request with
// no durable effect and remains local to that call.
type sessionStorageGuard struct {
	Storage
	check      func() error
	fail       func(error)
	failCommit func(error)
}

func (s sessionStorageGuard) record(err error) error {
	if err != nil && s.fail != nil {
		s.fail(err)
	}
	return err
}

func guardedStorageCall[T any](s sessionStorageGuard, call func() (T, error)) (T, error) {
	var zero T
	if s.check != nil {
		if err := s.check(); err != nil {
			return zero, err
		}
	}
	value, err := call()
	return value, s.record(err)
}

func (s sessionStorageGuard) Commit(ctx context.Context, writes []StorageWrite) (Seq, error) {
	if s.check != nil {
		if err := s.check(); err != nil {
			return 0, err
		}
	}
	seq, err := s.Storage.Commit(ctx, writes)
	if err != nil && s.failCommit != nil {
		s.failCommit(err)
	}
	return seq, err
}

func (s sessionStorageGuard) MintID(ctx context.Context) (Id, error) {
	return guardedStorageCall(s, func() (Id, error) { return s.Storage.MintID(ctx) })
}

func (s sessionStorageGuard) Conversation(ctx context.Context, id Id) (*ConversationRecord, error) {
	return guardedStorageCall(s, func() (*ConversationRecord, error) { return s.Storage.Conversation(ctx, id) })
}

func (s sessionStorageGuard) ScanConversations(ctx context.Context, query ConversationQuery, cursor Cursor, limit int) (Page[ConversationRecord], error) {
	return guardedStorageCall(s, func() (Page[ConversationRecord], error) {
		return s.Storage.ScanConversations(ctx, query, cursor, limit)
	})
}

func (s sessionStorageGuard) Entry(ctx context.Context, id Id) (*EntryCommit, error) {
	return guardedStorageCall(s, func() (*EntryCommit, error) { return s.Storage.Entry(ctx, id) })
}

func (s sessionStorageGuard) FindLatestHeadMarker(ctx context.Context, conversationID Id, atOrBeforeEntryID *Id) (*EntryRecord, error) {
	return guardedStorageCall(s, func() (*EntryRecord, error) {
		return s.Storage.FindLatestHeadMarker(ctx, conversationID, atOrBeforeEntryID)
	})
}

func (s sessionStorageGuard) ScanEntries(ctx context.Context, query EntryQuery, cursor Cursor, limit int) (Page[EntryRecord], error) {
	return guardedStorageCall(s, func() (Page[EntryRecord], error) {
		return s.Storage.ScanEntries(ctx, query, cursor, limit)
	})
}

func (s sessionStorageGuard) Task(ctx context.Context, id Id) (*TaskRecord, error) {
	return guardedStorageCall(s, func() (*TaskRecord, error) { return s.Storage.Task(ctx, id) })
}

func (s sessionStorageGuard) ScanTasks(ctx context.Context, query TaskQuery, cursor Cursor, limit int) (Page[TaskRecord], error) {
	return guardedStorageCall(s, func() (Page[TaskRecord], error) {
		return s.Storage.ScanTasks(ctx, query, cursor, limit)
	})
}

func (s sessionStorageGuard) Submission(ctx context.Context, id Id) (*SubmissionRecord, error) {
	return guardedStorageCall(s, func() (*SubmissionRecord, error) { return s.Storage.Submission(ctx, id) })
}

func (s sessionStorageGuard) ScanSubmissions(ctx context.Context, query SubmissionQuery, cursor Cursor, limit int) (Page[SubmissionRecord], error) {
	return guardedStorageCall(s, func() (Page[SubmissionRecord], error) {
		return s.Storage.ScanSubmissions(ctx, query, cursor, limit)
	})
}

func (s sessionStorageGuard) SubmissionByRequest(ctx context.Context, conversationID Id, requestID string) (*SubmissionRecord, error) {
	return guardedStorageCall(s, func() (*SubmissionRecord, error) {
		return s.Storage.SubmissionByRequest(ctx, conversationID, requestID)
	})
}

func (s sessionStorageGuard) FindDocument(ctx context.Context, address DocumentAddress, at DocumentPoint) (*DocumentRecord, error) {
	return guardedStorageCall(s, func() (*DocumentRecord, error) {
		return s.Storage.FindDocument(ctx, address, at)
	})
}

func (s sessionStorageGuard) Document(ctx context.Context, id Id, at DocumentPoint) (*StoredDocument, error) {
	return guardedStorageCall(s, func() (*StoredDocument, error) { return s.Storage.Document(ctx, id, at) })
}

func (s sessionStorageGuard) ScanDocuments(ctx context.Context, query DocumentQuery, cursor Cursor, limit int) (Page[DocumentRecord], error) {
	return guardedStorageCall(s, func() (Page[DocumentRecord], error) {
		return s.Storage.ScanDocuments(ctx, query, cursor, limit)
	})
}
