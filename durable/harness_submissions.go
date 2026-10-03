package durable

import (
	"encoding/json"
	"fmt"
	"sync/atomic"

	"github.com/dat267/pier/ai"
	"github.com/dat267/pier/chord"
)

// Port of harness/submissions.ts: admission, waits and withdrawal of the
// durable submissions of one harness.

// SubmissionDraft is user input that may start a run, or a passive entry
// write. Upstream's union is discriminated by Type.
type SubmissionDraft struct {
	RequestID *string
	// Type is "input" or "write".
	Type string
	// Content is the user input of an "input" draft.
	Content UserInput
	// WhenBusy is the input's busy policy: steer, followUp or reject.
	WhenBusy *string
	// Entry is the write of a "write" draft.
	Entry *EntryDraft
}

// Submissions admits, waits for and withdraws the durable submissions of one
// harness.
type Submissions struct {
	session    *Session
	storage    Storage
	now        func() int64
	queueModes func() QueueModes
	resume     func()
	waiters    *Waiters[Id, SubmissionRecord]
	closed     atomic.Bool
}

// NewSubmissions builds the submissions of one session and subscribes it to
// the session's publications and close.
func NewSubmissions(session *Session, storage Storage, now func() int64, queueModes func() QueueModes, resume func()) *Submissions {
	submissions := &Submissions{
		session: session, storage: storage, now: now, queueModes: queueModes, resume: resume,
		waiters: &Waiters[Id, SubmissionRecord]{},
	}
	session.SubscribeCommits(func(publication CommitPublication, ctx chord.Context) {
		submissions.observe(publication)
	})
	session.SubscribeClose(func() {
		submissions.closed.Store(true)
		submissions.waiters.RejectAll(ClosedError())
	})
	return submissions
}

// Submit admits a submission in one commit.
func (s *Submissions) Submit(conversationID Id, draft SubmissionDraft, ctx chord.Context) (Submission, error) {
	s.resume()
	var id Id
	err := s.session.Commit(ctx, func(tx *Transaction) error {
		created, err := AdmitSubmission(tx, conversationID, draft, s.now(), s.queueModes())
		id = created
		return err
	})
	if err != nil {
		return nil, err
	}
	return &SubmissionHandle{id: id, submissions: s}, nil
}

// Get returns the handle of an existing submission, or nil.
func (s *Submissions) Get(id Id, ctx chord.Context) (Submission, error) {
	var record *SubmissionRecord
	if err := s.session.ReadOnLine(func() error {
		var err error
		record, err = s.storage.Submission(ctx, id)
		return err
	}); err != nil {
		return nil, err
	}
	if record == nil {
		return nil, nil
	}
	return &SubmissionHandle{id: record.ID, submissions: s}, nil
}

// Status reads one submission record.
func (s *Submissions) Status(id Id, ctx chord.Context) (SubmissionRecord, error) {
	var record *SubmissionRecord
	if err := s.session.ReadOnLine(func() error {
		var err error
		record, err = s.storage.Submission(ctx, id)
		return err
	}); err != nil {
		return SubmissionRecord{}, err
	}
	if record == nil {
		return SubmissionRecord{}, fmt.Errorf("Submission %s does not exist", itoaID(id))
	}
	return *record, nil
}

// Wait resolves with the submission's terminal record.
func (s *Submissions) Wait(id Id, ctx chord.Context) (SubmissionRecord, error) {
	s.resume()
	// Check and register on the line so no settling publication falls between them.
	var await func() (SubmissionRecord, error)
	err := s.session.ReadOnLine(func() error {
		record, err := s.storage.Submission(ctx, id)
		if err != nil {
			return err
		}
		if record == nil {
			return fmt.Errorf("Submission %s does not exist", itoaID(id))
		}
		if isSettledSubmission(record) {
			settled := *record
			await = func() (SubmissionRecord, error) { return settled, nil }
			return nil
		}
		// Close rejects registered waiters synchronously and may begin during
		// the read.
		if s.closed.Load() {
			return ClosedError()
		}
		await = s.waiters.Register(id, ctx)
		return nil
	})
	if err != nil {
		return SubmissionRecord{}, err
	}
	return await()
}

// Abort withdraws a queued submission and removes its inbox item; placed and
// settled submissions are reported.
func (s *Submissions) Abort(id Id, ctx chord.Context, conversationID *Id) (string, error) {
	result := ""
	err := s.session.Commit(ctx, func(tx *Transaction) error {
		record, err := tx.Submission(id)
		if err != nil {
			return err
		}
		if record == nil || (conversationID != nil && record.ConversationID != *conversationID) {
			result = "not_found"
			return nil
		}
		if record.Status == SubmissionQueued {
			reason := "aborted"
			if err := tx.SettleSubmission(id, SubmissionSettlement{Status: SubmissionUnanswered, Reason: &reason}); err != nil {
				return err
			}
			if err := RemoveInboxItem(tx, record.ConversationID, id); err != nil {
				return err
			}
			result = "aborted"
			return nil
		}
		if record.Status == SubmissionPlaced {
			result = "already_placed"
			return nil
		}
		result = "settled"
		return nil
	})
	return result, err
}

func (s *Submissions) observe(publication CommitPublication) {
	for _, change := range publication.Changes {
		if change.Write == nil || change.Write.Type != "submission" {
			continue
		}
		record := change.Write.Submission
		if record == nil || !isSettledSubmission(record) {
			continue
		}
		s.waiters.Resolve(record.ID, *record)
	}
}

// SubmissionHandle is the awaitable handle of one admitted submission.
type SubmissionHandle struct {
	id          Id
	submissions *Submissions
}

func (h *SubmissionHandle) ID() Id { return h.id }

func (h *SubmissionHandle) Status(ctx chord.Context) (SubmissionRecord, error) {
	return h.submissions.Status(h.id, ctx)
}

func (h *SubmissionHandle) Wait(ctx chord.Context) (SubmissionRecord, error) {
	return h.submissions.Wait(h.id, ctx)
}

func (h *SubmissionHandle) Abort(ctx chord.Context) (string, error) {
	result, err := h.submissions.Abort(h.id, ctx, nil)
	if err != nil {
		return "", err
	}
	if result == "not_found" {
		return "", fmt.Errorf("Submission %s does not exist", itoaID(h.id))
	}
	return result, nil
}

func isSettledSubmission(record *SubmissionRecord) bool {
	return record != nil && (record.Status == SubmissionDone || record.Status == SubmissionUnanswered)
}

// AdmitSubmission admits a submission inside a commit (spec §6).
func AdmitSubmission(tx *Transaction, conversationID Id, draft SubmissionDraft, now int64, queueModes QueueModes) (Id, error) {
	if draft.RequestID != nil {
		existing, err := tx.SubmissionByRequest(conversationID, *draft.RequestID)
		if err != nil {
			return 0, err
		}
		if existing != nil {
			if existing.Type != draft.Type {
				return 0, fmt.Errorf("Request %s already identifies a submission of type %s", *draft.RequestID, existing.Type)
			}
			return existing.ID, nil
		}
	}
	live, err := tx.Doc(LiveDoc.Definition, conversationID)
	if err != nil {
		return 0, err
	}
	busy := liveRunPresent(live)
	if busy && draft.Type == SubmissionTypeInput && draft.WhenBusy != nil && *draft.WhenBusy == "reject" {
		return 0, &ConversationBusyError{ConversationID: conversationID}
	}
	requestID := draft.RequestID
	// A boundary reads the table, so it is prepared before the first table
	// write; a busy one needs none.
	var boundary *Boundary
	if !busy {
		boundary, err = PrepareBoundary(tx, conversationID, queueModes)
		if err != nil {
			return 0, err
		}
	}
	if boundary == nil || boundaryItemCount(boundary) > 0 {
		created, err := tx.CreateSubmission(SubmissionCreate{
			ConversationID: conversationID, RequestID: requestID, Type: draft.Type, Status: SubmissionQueued,
		})
		if err != nil {
			return 0, err
		}
		var inbox chord.JsonValue
		if boundary != nil {
			inbox = boundary.Inbox
		} else {
			inbox, err = tx.Doc(InboxDoc.Definition, conversationID)
			if err != nil {
				return 0, err
			}
		}
		item := InboxItem{ID: created.ID}
		if draft.Type == SubmissionTypeWrite {
			item.Mode = InboxWrite
			encoded, err := marshalJSONValue(*draft.Entry)
			if err != nil {
				return 0, err
			}
			item.Entry = json.RawMessage(encoded)
		} else {
			item.Mode = InboxFollowUp
			if draft.WhenBusy != nil && *draft.WhenBusy == "steer" {
				item.Mode = InboxSteer
			}
			item.Content = draft.Content
		}
		if err := AppendInboxItem(inbox, item); err != nil {
			return 0, err
		}
		if boundary == nil {
			return created.ID, nil
		}
		result, err := ApplyBoundary(tx, boundary, "final", now)
		if err != nil {
			return 0, err
		}
		if len(result.Users) > 0 {
			if err := StartRun(tx, conversationID, live, result.Users); err != nil {
				return 0, err
			}
		}
		return created.ID, nil
	}
	if draft.Type == SubmissionTypeWrite {
		if IsStale(boundary, *draft.Entry) {
			reason := "stale"
			created, err := tx.CreateSubmission(SubmissionCreate{
				ConversationID: conversationID, RequestID: requestID, Type: SubmissionTypeWrite,
				Status: SubmissionUnanswered, Reason: &reason,
			})
			if err != nil {
				return 0, err
			}
			return created.ID, nil
		}
		entry, err := tx.AppendEntry(conversationID, *draft.Entry)
		if err != nil {
			return 0, err
		}
		created, err := tx.CreateSubmission(SubmissionCreate{
			ConversationID: conversationID, RequestID: requestID, Type: SubmissionTypeWrite,
			Status: SubmissionDone, Entry: &entry.ID,
		})
		if err != nil {
			return 0, err
		}
		return created.ID, nil
	}
	message, err := userMessageFromContent(draft.Content, now)
	if err != nil {
		return 0, err
	}
	entry, err := tx.AppendEntry(conversationID, EntryDraft{Kind: UserEntry.Kind, Model: []ai.Message{message}})
	if err != nil {
		return 0, err
	}
	created, err := tx.CreateSubmission(SubmissionCreate{
		ConversationID: conversationID, RequestID: requestID, Type: SubmissionTypeInput,
		Status: SubmissionPlaced, Entry: &entry.ID,
	})
	if err != nil {
		return 0, err
	}
	if err := StartRun(tx, conversationID, live, []Id{created.ID}); err != nil {
		return 0, err
	}
	return created.ID, nil
}

func liveRunPresent(live chord.JsonValue) bool {
	object, ok := live.(map[string]any)
	if !ok {
		return false
	}
	return object["run"] != nil
}

func boundaryItemCount(boundary *Boundary) int {
	state, err := decodeInboxState(boundary.Inbox)
	if err != nil {
		return 0
	}
	return len(state.Items)
}
