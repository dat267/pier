package durable

import "context"

// sessionStorageGuard makes every Storage operation fail the owning Session on
// unexpected errors. StorageRejectedError represents an invalid request with
// no durable effect and remains local to that call.
type sessionStorageGuard struct {
	Storage
	fail       func(error)
	failCommit func(error)
}

func (s sessionStorageGuard) record(err error) error {
	if err != nil && s.fail != nil {
		s.fail(err)
	}
	return err
}

func (s sessionStorageGuard) Commit(ctx context.Context, writes []StorageWrite) (Seq, error) {
	seq, err := s.Storage.Commit(ctx, writes)
	if err != nil && s.failCommit != nil {
		s.failCommit(err)
	}
	return seq, err
}

func (s sessionStorageGuard) MintID(ctx context.Context) (Id, error) {
	id, err := s.Storage.MintID(ctx)
	return id, s.record(err)
}

func (s sessionStorageGuard) Conversation(ctx context.Context, id Id) (*ConversationRecord, error) {
	record, err := s.Storage.Conversation(ctx, id)
	return record, s.record(err)
}

func (s sessionStorageGuard) ScanConversations(ctx context.Context, query ConversationQuery, cursor Cursor, limit int) (Page[ConversationRecord], error) {
	page, err := s.Storage.ScanConversations(ctx, query, cursor, limit)
	return page, s.record(err)
}

func (s sessionStorageGuard) Entry(ctx context.Context, id Id) (*EntryCommit, error) {
	entry, err := s.Storage.Entry(ctx, id)
	return entry, s.record(err)
}

func (s sessionStorageGuard) FindLatestHeadMarker(ctx context.Context, conversationID Id, atOrBeforeEntryID *Id) (*EntryRecord, error) {
	entry, err := s.Storage.FindLatestHeadMarker(ctx, conversationID, atOrBeforeEntryID)
	return entry, s.record(err)
}

func (s sessionStorageGuard) ScanEntries(ctx context.Context, query EntryQuery, cursor Cursor, limit int) (Page[EntryRecord], error) {
	page, err := s.Storage.ScanEntries(ctx, query, cursor, limit)
	return page, s.record(err)
}

func (s sessionStorageGuard) Task(ctx context.Context, id Id) (*TaskRecord, error) {
	task, err := s.Storage.Task(ctx, id)
	return task, s.record(err)
}

func (s sessionStorageGuard) ScanTasks(ctx context.Context, query TaskQuery, cursor Cursor, limit int) (Page[TaskRecord], error) {
	page, err := s.Storage.ScanTasks(ctx, query, cursor, limit)
	return page, s.record(err)
}

func (s sessionStorageGuard) Submission(ctx context.Context, id Id) (*SubmissionRecord, error) {
	submission, err := s.Storage.Submission(ctx, id)
	return submission, s.record(err)
}

func (s sessionStorageGuard) ScanSubmissions(ctx context.Context, query SubmissionQuery, cursor Cursor, limit int) (Page[SubmissionRecord], error) {
	page, err := s.Storage.ScanSubmissions(ctx, query, cursor, limit)
	return page, s.record(err)
}

func (s sessionStorageGuard) SubmissionByRequest(ctx context.Context, conversationID Id, requestID string) (*SubmissionRecord, error) {
	submission, err := s.Storage.SubmissionByRequest(ctx, conversationID, requestID)
	return submission, s.record(err)
}

func (s sessionStorageGuard) FindDocument(ctx context.Context, address DocumentAddress, at DocumentPoint) (*DocumentRecord, error) {
	document, err := s.Storage.FindDocument(ctx, address, at)
	return document, s.record(err)
}

func (s sessionStorageGuard) Document(ctx context.Context, id Id, at DocumentPoint) (*StoredDocument, error) {
	document, err := s.Storage.Document(ctx, id, at)
	return document, s.record(err)
}

func (s sessionStorageGuard) ScanDocuments(ctx context.Context, query DocumentQuery, cursor Cursor, limit int) (Page[DocumentRecord], error) {
	page, err := s.Storage.ScanDocuments(ctx, query, cursor, limit)
	return page, s.record(err)
}
