package durable

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"
)

// Port of storage/jsonl (index.ts, storage.ts, node.ts): a log-structured
// Storage that appends one commit marker per commit to `main.jsonl` and keeps
// live snapshots in `task-<id>.jsonl` and `doc-<id>.jsonl` sidecars. Reads are
// served from an in-memory mirror rebuilt at open.
//
// The file format is upstream's: format version, marker/record shapes, sidecar
// naming, torn-tail truncation, `.reclaim` cleanup, confirmed-record
// truncation, reclamation of retired and superseded sidecars, and
// poison-on-append-failure. The upstream `FileSystem` abstraction is replaced
// by `os` (the other backends use their driver directly too).

// jsonlFormatVersion is upstream FORMAT_VERSION.
const jsonlFormatVersion = 1

// jsonlMainFile is the main log (upstream MAIN_FILE).
const jsonlMainFile = "main.jsonl"

// jsonlReclaimSuffix marks a sidecar rewrite in progress (upstream
// RECLAIM_SUFFIX).
const jsonlReclaimSuffix = ".reclaim"

// JsonlStorageOptions are OpenJsonlStorage inputs.
type JsonlStorageOptions struct {
	// Fsync flushes every affected sidecar before appending the marker.
	Fsync bool
}

// JsonlCorruptionError reports a log that violates the format.
type JsonlCorruptionError struct {
	Message string
	Cause   error
}

func (e *JsonlCorruptionError) Error() string { return e.Message }
func (e *JsonlCorruptionError) Unwrap() error { return e.Cause }

// JsonlStoragePoisonedError reports a storage that failed to append and must
// be reopened (upstream JsonlStoragePoisonedError).
type JsonlStoragePoisonedError struct {
	Cause error
}

func (e *JsonlStoragePoisonedError) Error() string {
	return "JSONL storage is poisoned and must be reopened"
}
func (e *JsonlStoragePoisonedError) Unwrap() error { return e.Cause }

// JsonlStorage is the portable JSONL Storage implementation.
type JsonlStorage struct {
	mu               sync.Mutex
	dir              string
	mainPath         string
	fsync            bool
	memory           *MemoryStorage
	liveTaskSidecars map[Id]bool
	currentOnlyDocs  map[Id]bool
	closed           bool
	poisoned         error
}

// OpenJsonlStorage opens or creates a JSONL storage directory and recovers its
// state (upstream JsonlStorage.open).
func OpenJsonlStorage(ctx context.Context, directory string, options JsonlStorageOptions) (*JsonlStorage, error) {
	_ = ctx
	absolute, err := filepath.Abs(directory)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(absolute, 0o755); err != nil {
		return nil, fmt.Errorf("JSONL directory creation failed: %s", err)
	}
	storage := &JsonlStorage{
		dir:              absolute,
		mainPath:         filepath.Join(absolute, jsonlMainFile),
		fsync:            options.Fsync,
		memory:           NewMemoryStorage(),
		liveTaskSidecars: map[Id]bool{},
		currentOnlyDocs:  map[Id]bool{},
	}
	if err := storage.recover(); err != nil {
		return nil, err
	}
	return storage, nil
}

// Commit appends the batch and reports its sequence (upstream commit).
func (s *JsonlStorage) Commit(ctx context.Context, writes []StorageWrite) (Seq, error) {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.assertUsable(); err != nil {
		return 0, err
	}
	prepared, err := s.memory.PrepareCommit(writes)
	if err != nil {
		return 0, err
	}
	marker, sidecars, err := encodeJsonlCommit(prepared)
	if err != nil {
		return 0, err
	}
	reclamations := s.planReclamations(prepared.Writes, sidecars)

	for _, file := range sortedSidecarNames(sidecars) {
		path := filepath.Join(s.dir, file)
		if err := appendFile(path, []byte(sidecars[file])); err != nil {
			return 0, s.poison(fmt.Errorf("JSONL append to %s failed: %s", file, err))
		}
		if s.fsync {
			if err := flushFile(path); err != nil {
				return 0, s.poison(fmt.Errorf("JSONL flush of %s failed: %s", file, err))
			}
		}
	}
	if err := appendFile(s.mainPath, []byte(marker)); err != nil {
		return 0, s.poison(fmt.Errorf("JSONL append to %s failed: %s", jsonlMainFile, err))
	}
	seq := prepared.Apply()
	s.adoptSidecarState(prepared.Writes)
	s.reclaimSidecars(reclamations)
	return seq, nil
}

// MintID returns the next id (upstream mintId).
func (s *JsonlStorage) MintID(ctx context.Context) (Id, error) {
	s.mu.Lock()
	if err := s.assertUsable(); err != nil {
		s.mu.Unlock()
		return 0, err
	}
	s.mu.Unlock()
	return s.memory.MintID(ctx)
}

// Conversation looks up one conversation.
func (s *JsonlStorage) Conversation(ctx context.Context, id Id) (*ConversationRecord, error) {
	if err := s.assertUsable(); err != nil {
		return nil, err
	}
	return s.memory.Conversation(ctx, id)
}

// ScanConversations scans conversations.
func (s *JsonlStorage) ScanConversations(ctx context.Context, cursor Cursor, limit int) (Page[ConversationRecord], error) {
	if err := s.assertUsable(); err != nil {
		return Page[ConversationRecord]{}, err
	}
	return s.memory.ScanConversations(ctx, cursor, limit)
}

// Entry looks up one entry.
func (s *JsonlStorage) Entry(ctx context.Context, id Id) (*EntryCommit, error) {
	if err := s.assertUsable(); err != nil {
		return nil, err
	}
	return s.memory.Entry(ctx, id)
}

// FindLatestHeadMarker finds the newest head marker at or below the cutoff.
func (s *JsonlStorage) FindLatestHeadMarker(ctx context.Context, conversationID Id, atOrBeforeEntryID *Id) (*EntryRecord, error) {
	if err := s.assertUsable(); err != nil {
		return nil, err
	}
	return s.memory.FindLatestHeadMarker(ctx, conversationID, atOrBeforeEntryID)
}

// ScanEntries scans entries.
func (s *JsonlStorage) ScanEntries(ctx context.Context, query EntryQuery, cursor Cursor, limit int) (Page[EntryRecord], error) {
	if err := s.assertUsable(); err != nil {
		return Page[EntryRecord]{}, err
	}
	return s.memory.ScanEntries(ctx, query, cursor, limit)
}

// Task looks up one task.
func (s *JsonlStorage) Task(ctx context.Context, id Id) (*TaskRecord, error) {
	if err := s.assertUsable(); err != nil {
		return nil, err
	}
	return s.memory.Task(ctx, id)
}

// ScanTasks scans tasks.
func (s *JsonlStorage) ScanTasks(ctx context.Context, query TaskQuery, cursor Cursor, limit int) (Page[TaskRecord], error) {
	if err := s.assertUsable(); err != nil {
		return Page[TaskRecord]{}, err
	}
	return s.memory.ScanTasks(ctx, query, cursor, limit)
}

// Submission looks up one submission.
func (s *JsonlStorage) Submission(ctx context.Context, id Id) (*SubmissionRecord, error) {
	if err := s.assertUsable(); err != nil {
		return nil, err
	}
	return s.memory.Submission(ctx, id)
}

// SubmissionByRequest finds a submission by its conversation-scoped request
// key.
func (s *JsonlStorage) SubmissionByRequest(ctx context.Context, conversationID Id, requestID string) (*SubmissionRecord, error) {
	if err := s.assertUsable(); err != nil {
		return nil, err
	}
	return s.memory.SubmissionByRequest(ctx, conversationID, requestID)
}

// FindDocument returns the incarnation alive at the point for an exact address.
func (s *JsonlStorage) FindDocument(ctx context.Context, address DocumentAddress, at DocumentPoint) (*DocumentRecord, error) {
	if err := s.assertUsable(); err != nil {
		return nil, err
	}
	return s.memory.FindDocument(ctx, address, at)
}

// Document materializes one incarnation at the point.
func (s *JsonlStorage) Document(ctx context.Context, id Id, at DocumentPoint) (*StoredDocument, error) {
	if err := s.assertUsable(); err != nil {
		return nil, err
	}
	return s.memory.Document(ctx, id, at)
}

// ScanDocuments scans the incarnations alive in one scope at the point.
func (s *JsonlStorage) ScanDocuments(ctx context.Context, query DocumentQuery, cursor Cursor, limit int) (Page[DocumentRecord], error) {
	if err := s.assertUsable(); err != nil {
		return Page[DocumentRecord]{}, err
	}
	return s.memory.ScanDocuments(ctx, query, cursor, limit)
}

// Close stops the storage (upstream close).
func (s *JsonlStorage) Close(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	return s.memory.Close(ctx)
}

func (s *JsonlStorage) assertUsable() error {
	if s.closed {
		return fmt.Errorf("JsonlStorage is closed")
	}
	if s.poisoned != nil {
		return s.poisoned
	}
	return nil
}

func (s *JsonlStorage) poison(cause error) error {
	if s.poisoned == nil {
		s.poisoned = &JsonlStoragePoisonedError{Cause: cause}
	}
	return s.poisoned
}

// --- encoding ---

// jsonlMainMarker is one commit line (upstream MainMarker).
type jsonlMainMarker struct {
	Format int                  `json:"format"`
	Type   string               `json:"type"`
	Seq    Id                   `json:"seq"`
	Writes []jsonlMainOperation `json:"writes"`
}

// jsonlMainOperation is one write of a commit marker (upstream MainOperation).
type jsonlMainOperation struct {
	Type    string          `json:"type"`
	Value   json.RawMessage `json:"value,omitempty"`
	ID      *Id             `json:"id,omitempty"`
	Ordinal *int            `json:"ordinal,omitempty"`
	Record  json.RawMessage `json:"record,omitempty"`
}

// jsonlSidecarRecord is one sidecar line (upstream SidecarRecord).
type jsonlSidecarRecord struct {
	Format  int                 `json:"format"`
	Type    string              `json:"type"`
	Seq     Id                  `json:"seq"`
	Ordinal int                 `json:"ordinal"`
	Payload jsonlSidecarPayload `json:"payload"`
}

// jsonlSidecarPayload carries a live task or document snapshot.
type jsonlSidecarPayload struct {
	Type    string          `json:"type"`
	Value   json.RawMessage `json:"value,omitempty"`
	ID      *Id             `json:"id,omitempty"`
	Content json.RawMessage `json:"content,omitempty"`
}

// encodeJsonlCommit builds the marker line and the sidecar lines for one batch
// (upstream encodeCommit).
func encodeJsonlCommit(prepared *PreparedCommit) (string, map[string]string, error) {
	mainWrites := make([]jsonlMainOperation, 0, len(prepared.Writes))
	records := map[string][]jsonlSidecarRecord{}
	nextOrdinal := 0
	addSidecar := func(file string, payload jsonlSidecarPayload) int {
		ordinal := nextOrdinal
		nextOrdinal++
		records[file] = append(records[file], jsonlSidecarRecord{
			Format: jsonlFormatVersion, Type: "record", Seq: prepared.Seq, Ordinal: ordinal, Payload: payload,
		})
		return ordinal
	}
	for _, write := range prepared.Writes {
		switch write.Type {
		case "conversation", "submission":
			value, err := json.Marshal(writeValue(write))
			if err != nil {
				return "", nil, err
			}
			mainWrites = append(mainWrites, jsonlMainOperation{Type: write.Type, Value: value})
		case "entry":
			value, err := json.Marshal(write.Entry)
			if err != nil {
				return "", nil, err
			}
			mainWrites = append(mainWrites, jsonlMainOperation{Type: "entry", Value: value})
		case "task":
			value, err := json.Marshal(write.Task)
			if err != nil {
				return "", nil, err
			}
			if write.Task.State.Status == TaskTerminal {
				mainWrites = append(mainWrites, jsonlMainOperation{Type: "task", Value: value})
				continue
			}
			ordinal := addSidecar(jsonlSidecarFileName("task", write.Task.ID), jsonlSidecarPayload{Type: "task", Value: value})
			id := write.Task.ID
			mainWrites = append(mainWrites, jsonlMainOperation{Type: "task.sidecar", ID: &id, Ordinal: &ordinal})
		case "document.create", "document.change":
			if write.DocumentContent == nil {
				return "", nil, fmt.Errorf("%s without content", write.Type)
			}
			var id Id
			if write.Type == "document.create" {
				if write.DocumentCreate == nil {
					return "", nil, errors.New("document.create without a record")
				}
				id = write.DocumentCreate.ID
			} else {
				if write.DocumentID == nil {
					return "", nil, errors.New("document.change without an id")
				}
				id = *write.DocumentID
			}
			content, err := json.Marshal(*write.DocumentContent)
			if err != nil {
				return "", nil, err
			}
			ordinal := addSidecar(jsonlSidecarFileName("doc", id), jsonlSidecarPayload{
				Type: "document", ID: &id, Content: content,
			})
			operation := jsonlMainOperation{Type: write.Type, ID: &id, Ordinal: &ordinal}
			if write.Type == "document.create" {
				record, err := json.Marshal(*write.DocumentCreate)
				if err != nil {
					return "", nil, err
				}
				operation.Record = record
			}
			mainWrites = append(mainWrites, operation)
		case "document.retire":
			if write.DocumentID == nil {
				return "", nil, errors.New("document.retire without an id")
			}
			mainWrites = append(mainWrites, jsonlMainOperation{Type: "document.retire", ID: write.DocumentID})
		case "document.copy":
			return "", nil, errors.New("unresolved document.copy write")
		default:
			return "", nil, fmt.Errorf("unknown storage write type: %s", write.Type)
		}
	}
	sidecars := map[string]string{}
	for file, fileRecords := range records {
		var builder strings.Builder
		for _, record := range fileRecords {
			encoded, err := json.Marshal(record)
			if err != nil {
				return "", nil, err
			}
			builder.Write(encoded)
			builder.WriteByte('\n')
		}
		sidecars[file] = builder.String()
	}
	encodedMarker, err := json.Marshal(jsonlMainMarker{
		Format: jsonlFormatVersion, Type: "commit", Seq: prepared.Seq, Writes: mainWrites,
	})
	if err != nil {
		return "", nil, err
	}
	return string(encodedMarker) + "\n", sidecars, nil
}

// writeValue is the persisted value of a conversation/submission write.
func writeValue(write StorageWrite) any {
	if write.Type == "submission" {
		return write.Submission
	}
	return write.Conversation
}

// jsonlSidecarFileName is upstream sidecarFileName (documents unsupported, but
// the name shape is kept for parity).
func jsonlSidecarFileName(kind string, id Id) string {
	return kind + "-" + strconv.FormatInt(int64(id), 10) + ".jsonl"
}

// isJsonlSidecarFileName reports `task-<n>.jsonl` / `doc-<n>.jsonl`.
func isJsonlSidecarFileName(name string) bool {
	if strings.HasSuffix(name, jsonlReclaimSuffix) {
		return false
	}
	return sidecarIDSuffix(name) != nil
}

// isJsonlReclaimFileName reports the in-progress rewrite files.
func isJsonlReclaimFileName(name string) bool {
	if !strings.HasSuffix(name, jsonlReclaimSuffix) {
		return false
	}
	return sidecarIDSuffix(strings.TrimSuffix(name, jsonlReclaimSuffix)) != nil
}

// sidecarIDSuffix parses the numeric id of a sidecar file name.
func sidecarIDSuffix(name string) *Id {
	if !strings.HasSuffix(name, ".jsonl") {
		return nil
	}
	base := strings.TrimSuffix(name, ".jsonl")
	prefix, digits, found := strings.Cut(base, "-")
	if !found || (prefix != "doc" && prefix != "task") || digits == "" {
		return nil
	}
	if len(digits) > 1 && digits[0] == '0' {
		return nil
	}
	for _, r := range digits {
		if r < '0' || r > '9' {
			return nil
		}
	}
	value, err := strconv.ParseInt(digits, 10, 64)
	if err != nil {
		return nil
	}
	id := Id(value)
	return &id
}

// jsonlSidecarKey identifies one sidecar record (upstream sidecarKey).
func jsonlSidecarKey(file string, seq Id, ordinal int) string {
	encoded, _ := json.Marshal([]any{file, seq, ordinal})
	return string(encoded)
}

// planReclamations decides which sidecars to drop or rewrite after a commit
// (upstream planReclamations; documents are unsupported).
func (s *JsonlStorage) planReclamations(writes []StorageWrite, sidecars map[string]string) map[string]string {
	createdCurrentOnly := map[Id]bool{}
	retired := map[Id]bool{}
	baseDocuments := map[Id]bool{}
	finalTasks := map[Id]*TaskRecord{}
	for _, write := range writes {
		switch write.Type {
		case "document.create":
			if write.DocumentCreate != nil && isCurrentOnlyDocument(&DocumentRecord{
				Scope: write.DocumentCreate.Scope, History: write.DocumentCreate.History, Fork: write.DocumentCreate.Fork,
			}) {
				createdCurrentOnly[write.DocumentCreate.ID] = true
			}
		case "document.change":
			if write.DocumentID != nil && write.DocumentContent != nil && write.DocumentContent.Kind == ContentBase {
				baseDocuments[*write.DocumentID] = true
			}
		case "document.retire":
			if write.DocumentID != nil {
				retired[*write.DocumentID] = true
			}
		case "task":
			finalTasks[write.Task.ID] = write.Task
		}
	}
	isCurrentOnly := func(id Id) bool { return s.currentOnlyDocs[id] || createdCurrentOnly[id] }
	replacements := map[string]string{}
	for id := range retired {
		if isCurrentOnly(id) {
			replacements[jsonlSidecarFileName("doc", id)] = ""
		}
	}
	for id := range baseDocuments {
		if !isCurrentOnly(id) || retired[id] {
			continue
		}
		file := jsonlSidecarFileName("doc", id)
		if content := sidecars[file]; content != "" {
			replacements[file] = content
		}
	}
	for id, task := range finalTasks {
		if task.State.Status == TaskTerminal && (s.liveTaskSidecars[id] || sidecars[jsonlSidecarFileName("task", id)] != "") {
			replacements[jsonlSidecarFileName("task", id)] = ""
		}
	}
	return replacements
}

// adoptSidecarState records which tasks keep a live sidecar.
func (s *JsonlStorage) adoptSidecarState(writes []StorageWrite) {
	for _, write := range writes {
		switch write.Type {
		case "task":
			if write.Task.State.Status == TaskTerminal {
				delete(s.liveTaskSidecars, write.Task.ID)
			} else {
				s.liveTaskSidecars[write.Task.ID] = true
			}
		case "document.create":
			if write.DocumentCreate != nil && isCurrentOnlyDocument(&DocumentRecord{
				Scope: write.DocumentCreate.Scope, History: write.DocumentCreate.History, Fork: write.DocumentCreate.Fork,
			}) {
				s.currentOnlyDocs[write.DocumentCreate.ID] = true
			}
		}
	}
}

// reclaimSidecars applies the planned sidecar replacements. The marker already
// published the state, so reclamation is best-effort (upstream
// reclaimSidecars).
func (s *JsonlStorage) reclaimSidecars(replacements map[string]string) {
	if len(replacements) == 0 {
		return
	}
	if s.fsync {
		if err := flushFile(s.mainPath); err != nil {
			return
		}
	}
	for file, content := range replacements {
		s.replaceSidecar(file, content)
	}
}

func (s *JsonlStorage) replaceSidecar(file string, content string) {
	path := filepath.Join(s.dir, file)
	if content == "" {
		_ = os.Remove(path)
		return
	}
	temporary := path + jsonlReclaimSuffix
	if err := os.WriteFile(temporary, []byte(content), 0o644); err != nil {
		return
	}
	if s.fsync {
		if err := flushFile(temporary); err != nil {
			return
		}
	}
	_ = os.Rename(temporary, path)
}

// --- recovery ---

// recover rebuilds the in-memory mirror from the log (upstream recover).
func (s *JsonlStorage) recover() error {
	mainLines, err := readJsonlLines(s.mainPath, jsonlMainFile, parseJsonlMainMarker)
	if err != nil {
		return err
	}
	previousSeq := Seq(0)
	for _, line := range mainLines {
		if line.value.Seq <= previousSeq {
			return &JsonlCorruptionError{Message: "Commit sequence does not strictly increase in " + jsonlMainFile}
		}
		previousSeq = line.value.Seq
	}

	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return fmt.Errorf("JSONL directory listing failed: %s", err)
	}
	for _, entry := range entries {
		if !entry.IsDir() && isJsonlReclaimFileName(entry.Name()) {
			_ = os.Remove(filepath.Join(s.dir, entry.Name()))
		}
	}
	sidecarFiles := []string{}
	for _, entry := range entries {
		if !entry.IsDir() && isJsonlSidecarFileName(entry.Name()) {
			sidecarFiles = append(sidecarFiles, entry.Name())
		}
	}
	sort.Strings(sidecarFiles)

	recordByKey := map[string]parsedJsonlLine[jsonlSidecarRecord]{}
	parsedFiles := map[string][]parsedJsonlLine[jsonlSidecarRecord]{}
	for _, file := range sidecarFiles {
		lines, err := readJsonlLines(filepath.Join(s.dir, file), file, parseJsonlSidecarRecord)
		if err != nil {
			return err
		}
		parsedFiles[file] = lines
		var previous *jsonlSidecarRecord
		for _, line := range lines {
			if previous != nil && (line.value.Seq < previous.Seq ||
				(line.value.Seq == previous.Seq && line.value.Ordinal <= previous.Ordinal)) {
				return &JsonlCorruptionError{Message: "Sidecar records are out of order in " + file}
			}
			previous = &line.value
			recordByKey[jsonlSidecarKey(file, line.value.Seq, line.value.Ordinal)] = line
		}
	}

	// The final state of every task and document address decides which sidecar
	// records are optional (reclaimed) and which sidecars can be dropped.
	currentOnlyDocuments := map[Id]bool{}
	retiredDocuments := map[Id]bool{}
	finalTaskIsLive := map[Id]bool{}
	for _, line := range mainLines {
		for _, operation := range line.value.Writes {
			switch operation.Type {
			case "task":
				var task TaskRecord
				if err := json.Unmarshal(operation.Value, &task); err != nil {
					return &JsonlCorruptionError{Message: "Invalid task write in " + jsonlMainFile, Cause: err}
				}
				finalTaskIsLive[task.ID] = false
			case "task.sidecar":
				if operation.ID != nil {
					finalTaskIsLive[*operation.ID] = true
				}
			case "document.create":
				record, err := parseJsonlDocumentCreate(operation.Record)
				if err != nil {
					return err
				}
				if isCurrentOnlyDocument(&DocumentRecord{Scope: record.Scope, History: record.History, Fork: record.Fork}) {
					currentOnlyDocuments[record.ID] = true
				}
			case "document.retire":
				if operation.ID != nil {
					retiredDocuments[*operation.ID] = true
				}
			}
		}
	}
	retiredCurrentOnly := map[Id]bool{}
	for id := range retiredDocuments {
		if currentOnlyDocuments[id] {
			retiredCurrentOnly[id] = true
		}
	}
	terminalTasks := map[Id]bool{}
	for id, live := range finalTaskIsLive {
		if !live {
			terminalTasks[id] = true
		}
	}

	// The newest stored base of a current-only document makes every earlier
	// record optional: a reclaimed base is replaced by the marker's record.
	latestBases := map[Id]jsonlSidecarRecord{}
	for _, line := range mainLines {
		for _, operation := range line.value.Writes {
			var id Id
			switch operation.Type {
			case "document.create":
				record, err := parseJsonlDocumentCreate(operation.Record)
				if err != nil {
					return err
				}
				id = record.ID
			case "document.change":
				if operation.ID == nil {
					continue
				}
				id = *operation.ID
			default:
				continue
			}
			if !currentOnlyDocuments[id] || operation.Ordinal == nil {
				continue
			}
			record, ok := recordByKey[jsonlSidecarKey(jsonlSidecarFileName("doc", id), line.value.Seq, *operation.Ordinal)]
			if !ok || record.value.Payload.Type != "document" || record.value.Payload.ID == nil || *record.value.Payload.ID != id {
				continue
			}
			var content DocumentContent
			if err := json.Unmarshal(record.value.Payload.Content, &content); err != nil || content.Kind != ContentBase {
				continue
			}
			previous, exists := latestBases[id]
			if !exists || record.value.Seq > previous.Seq ||
				(record.value.Seq == previous.Seq && record.value.Ordinal > previous.Ordinal) {
				latestBases[id] = record.value
			}
		}
	}
	isBeforeLatestBase := func(id Id, seq Id, ordinal int) bool {
		base, ok := latestBases[id]
		if !ok {
			return false
		}
		return seq < base.Seq || (seq == base.Seq && ordinal < base.Ordinal)
	}

	confirmed := map[string]bool{}
	for _, line := range mainLines {
		marker := line.value
		writes := []StorageWrite{}
		for _, operation := range marker.Writes {
			switch operation.Type {
			case "conversation":
				var value ConversationRecord
				if err := json.Unmarshal(operation.Value, &value); err != nil {
					return &JsonlCorruptionError{Message: fmt.Sprintf("Invalid conversation write in commit %d", marker.Seq), Cause: err}
				}
				writes = append(writes, StorageWrite{Type: "conversation", Conversation: &value})
			case "entry":
				var value EntryRecord
				if err := json.Unmarshal(operation.Value, &value); err != nil {
					return &JsonlCorruptionError{Message: fmt.Sprintf("Invalid entry write in commit %d", marker.Seq), Cause: err}
				}
				writes = append(writes, StorageWrite{Type: "entry", Entry: &value})
			case "submission":
				var value SubmissionRecord
				if err := json.Unmarshal(operation.Value, &value); err != nil {
					return &JsonlCorruptionError{Message: fmt.Sprintf("Invalid submission write in commit %d", marker.Seq), Cause: err}
				}
				writes = append(writes, StorageWrite{Type: "submission", Submission: &value})
			case "task":
				var value TaskRecord
				if err := json.Unmarshal(operation.Value, &value); err != nil {
					return &JsonlCorruptionError{Message: fmt.Sprintf("Invalid terminal task write in commit %d", marker.Seq), Cause: err}
				}
				writes = append(writes, StorageWrite{Type: "task", Task: &value})
			case "task.sidecar":
				if operation.ID == nil || operation.Ordinal == nil {
					return &JsonlCorruptionError{Message: fmt.Sprintf("Invalid task sidecar write in commit %d", marker.Seq)}
				}
				id := *operation.ID
				optional := terminalTasks[id]
				record, err := confirmJsonlRecord(marker.Seq, *operation.Ordinal, jsonlSidecarFileName("task", id), recordByKey, confirmed, optional)
				if err != nil {
					return err
				}
				if record == nil {
					continue
				}
				if record.Payload.Type != "task" {
					return &JsonlCorruptionError{Message: fmt.Sprintf("Confirmed task sidecar data does not match commit %d", marker.Seq)}
				}
				var value TaskRecord
				if err := json.Unmarshal(record.Payload.Value, &value); err != nil || value.ID != id {
					return &JsonlCorruptionError{Message: fmt.Sprintf("Confirmed task sidecar data does not match commit %d", marker.Seq)}
				}
				if !optional {
					writes = append(writes, StorageWrite{Type: "task", Task: &value})
				}
			case "document.create", "document.change":
				if operation.Ordinal == nil {
					return &JsonlCorruptionError{Message: fmt.Sprintf("Invalid document write in commit %d", marker.Seq)}
				}
				var id Id
				var record *DocumentCreate
				if operation.Type == "document.create" {
					parsed, err := parseJsonlDocumentCreate(operation.Record)
					if err != nil {
						return err
					}
					record = parsed
					id = parsed.ID
				} else {
					if operation.ID == nil {
						return &JsonlCorruptionError{Message: fmt.Sprintf("Invalid document change in commit %d", marker.Seq)}
					}
					id = *operation.ID
				}
				reclaimed := retiredCurrentOnly[id] || isBeforeLatestBase(id, marker.Seq, *operation.Ordinal)
				sidecarRecord, err := confirmJsonlRecord(marker.Seq, *operation.Ordinal, jsonlSidecarFileName("doc", id), recordByKey, confirmed, reclaimed)
				if err != nil {
					return err
				}
				var content *DocumentContent
				if sidecarRecord != nil {
					if sidecarRecord.Payload.Type != "document" || sidecarRecord.Payload.ID == nil || *sidecarRecord.Payload.ID != id {
						return &JsonlCorruptionError{Message: fmt.Sprintf("Confirmed document sidecar data does not match commit %d", marker.Seq)}
					}
					var decoded DocumentContent
					if err := json.Unmarshal(sidecarRecord.Payload.Content, &decoded); err != nil {
						return &JsonlCorruptionError{Message: fmt.Sprintf("Invalid document content in commit %d", marker.Seq), Cause: err}
					}
					content = &decoded
				}
				if operation.Type == "document.create" {
					if content != nil && content.Kind != ContentBase {
						return &JsonlCorruptionError{Message: fmt.Sprintf("Document creation lacks a confirmed base in commit %d", marker.Seq)}
					}
					use := DocumentContent{Version: 1, Kind: ContentBase, Value: json.RawMessage("{}")}
					if !reclaimed && content != nil {
						use = *content
					}
					writes = append(writes, StorageWrite{Type: "document.create", DocumentCreate: record, DocumentContent: &use})
				} else if !reclaimed && content != nil {
					writes = append(writes, StorageWrite{Type: "document.change", DocumentID: &id, DocumentContent: content})
				}
			case "document.retire":
				if operation.ID == nil {
					return &JsonlCorruptionError{Message: fmt.Sprintf("Invalid document retirement in commit %d", marker.Seq)}
				}
				writes = append(writes, StorageWrite{Type: "document.retire", DocumentID: operation.ID})
			default:
				return &JsonlCorruptionError{Message: fmt.Sprintf("Unknown write type in %s: %s", jsonlMainFile, operation.Type)}
			}
		}
		prepared, err := s.memory.PrepareCommitAt(writes, marker.Seq)
		if err != nil {
			return &JsonlCorruptionError{Message: fmt.Sprintf("Invalid committed state at sequence %d", marker.Seq), Cause: err}
		}
		prepared.Apply()
	}

	// Truncate unconfirmed sidecar tails and reclaim sidecars the log no longer
	// needs.
	reclamations := map[string]string{}
	for file, lines := range parsedFiles {
		unconfirmedAt := int64(-1)
		for _, line := range lines {
			key := jsonlSidecarKey(file, line.value.Seq, line.value.Ordinal)
			if confirmed[key] {
				if unconfirmedAt >= 0 {
					return &JsonlCorruptionError{Message: "Confirmed record follows an unconfirmed tail in " + file}
				}
				continue
			}
			if unconfirmedAt < 0 {
				unconfirmedAt = line.start
			}
		}
		if unconfirmedAt >= 0 {
			if err := os.Truncate(filepath.Join(s.dir, file), unconfirmedAt); err != nil {
				return fmt.Errorf("JSONL tail truncation of %s failed: %s", file, err)
			}
		}
		id := sidecarIDSuffix(file)
		var retained []string
		if id == nil {
			continue
		}
		if strings.HasPrefix(file, "task-") && terminalTasks[*id] {
			retained = []string{}
		} else if strings.HasPrefix(file, "doc-") {
			switch {
			case retiredCurrentOnly[*id]:
				retained = []string{}
			case latestBases[*id].Payload.Type != "":
				for _, line := range lines {
					if !confirmed[jsonlSidecarKey(file, line.value.Seq, line.value.Ordinal)] {
						continue
					}
					if isBeforeLatestBase(*id, line.value.Seq, line.value.Ordinal) {
						continue
					}
					encoded, err := json.Marshal(line.value)
					if err != nil {
						return err
					}
					retained = append(retained, string(encoded)+"\n")
				}
			}
		}
		if retained != nil {
			reclamations[file] = strings.Join(retained, "")
		}
	}
	s.reclaimSidecars(reclamations)

	for id := range currentOnlyDocuments {
		s.currentOnlyDocs[id] = true
	}
	for id, live := range finalTaskIsLive {
		if live {
			s.liveTaskSidecars[id] = true
		}
	}
	return nil
}

// parseJsonlDocumentCreate parses and validates a document.create record.
func parseJsonlDocumentCreate(raw json.RawMessage) (*DocumentCreate, error) {
	var record DocumentCreate
	if err := json.Unmarshal(raw, &record); err != nil || record.ID < 1 {
		return nil, &JsonlCorruptionError{Message: "Invalid document creation in " + jsonlMainFile}
	}
	return &record, nil
}

// confirmJsonlRecord marks one sidecar record as confirmed, requiring it unless
// it was reclaimed (upstream confirmRecord).
func confirmJsonlRecord(
	seq Id,
	ordinal int,
	file string,
	recordByKey map[string]parsedJsonlLine[jsonlSidecarRecord],
	confirmed map[string]bool,
	optional bool,
) (*jsonlSidecarRecord, error) {
	key := jsonlSidecarKey(file, seq, ordinal)
	if confirmed[key] {
		return nil, &JsonlCorruptionError{Message: "Sidecar record is confirmed more than once"}
	}
	line, ok := recordByKey[key]
	if !ok {
		if optional {
			return nil, nil
		}
		return nil, &JsonlCorruptionError{Message: fmt.Sprintf("Missing confirmed sidecar record %s at sequence %d", file, seq)}
	}
	confirmed[key] = true
	return &line.value, nil
}

// parsedJsonlLine is one parsed line with its byte offset.
type parsedJsonlLine[T any] struct {
	value T
	start int64
}

// readJsonlLines reads a JSONL file, truncating a torn last line (upstream
// readLines).
func readJsonlLines[T any](path string, name string, parse func([]byte, int) (T, error)) ([]parsedJsonlLine[T], error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("JSONL read of %s failed: %s", name, err)
	}
	completeSize := len(data)
	if completeSize > 0 && data[completeSize-1] != '\n' {
		completeSize = bytes.LastIndexByte(data, '\n') + 1
		if err := os.Truncate(path, int64(completeSize)); err != nil {
			return nil, fmt.Errorf("JSONL torn-line truncation of %s failed: %s", name, err)
		}
	}
	lines := []parsedJsonlLine[T]{}
	start := 0
	lineNumber := 1
	for end := 0; end < completeSize; end++ {
		if data[end] != '\n' {
			continue
		}
		if !utf8Valid(data[start:end]) {
			return nil, &JsonlCorruptionError{Message: fmt.Sprintf("Invalid UTF-8 in complete %s line %d", name, lineNumber)}
		}
		value, err := parse(data[start:end], lineNumber)
		if err != nil {
			return nil, err
		}
		lines = append(lines, parsedJsonlLine[T]{value: value, start: int64(start)})
		start = end + 1
		lineNumber++
	}
	return lines, nil
}

// parseJsonlMainMarker validates one commit marker (upstream parseMainMarker).
func parseJsonlMainMarker(text []byte, line int) (jsonlMainMarker, error) {
	description := fmt.Sprintf("%s line %d", jsonlMainFile, line)
	var marker jsonlMainMarker
	if err := json.Unmarshal(text, &marker); err != nil {
		return marker, &JsonlCorruptionError{Message: "Malformed complete " + description, Cause: err}
	}
	if marker.Format != jsonlFormatVersion || marker.Type != "commit" || marker.Seq < 1 || marker.Writes == nil {
		return marker, &JsonlCorruptionError{Message: "Invalid commit marker in " + description}
	}
	for _, write := range marker.Writes {
		switch write.Type {
		case "conversation", "entry", "submission", "task":
			var value struct {
				ID *Id `json:"id"`
			}
			if err := json.Unmarshal(write.Value, &value); err != nil || value.ID == nil {
				return marker, &JsonlCorruptionError{Message: fmt.Sprintf("Invalid %s write in %s", write.Type, description)}
			}
			if write.Type == "task" {
				var task TaskRecord
				if err := json.Unmarshal(write.Value, &task); err != nil || task.State.Status != TaskTerminal {
					return marker, &JsonlCorruptionError{Message: "Invalid terminal task write in " + description}
				}
			}
		case "task.sidecar":
			if write.ID == nil || write.Ordinal == nil || *write.Ordinal < 0 {
				return marker, &JsonlCorruptionError{Message: "Invalid task sidecar write in " + description}
			}
		case "document.retire":
			if write.ID == nil {
				return marker, &JsonlCorruptionError{Message: "Invalid document retirement in " + description}
			}
		case "document.create":
			if _, err := parseJsonlDocumentCreate(write.Record); err != nil {
				return marker, &JsonlCorruptionError{Message: "Invalid document creation in " + description}
			}
			if write.Ordinal == nil || *write.Ordinal < 0 {
				return marker, &JsonlCorruptionError{Message: "Invalid document creation in " + description}
			}
		case "document.change":
			if write.ID == nil || write.Ordinal == nil || *write.Ordinal < 0 {
				return marker, &JsonlCorruptionError{Message: "Invalid document change in " + description}
			}
		default:
			return marker, &JsonlCorruptionError{Message: "Unknown write type in " + description}
		}
	}
	return marker, nil
}

// parseJsonlSidecarRecord validates one sidecar line (upstream
// parseSidecarRecord).
func parseJsonlSidecarRecord(text []byte, line int) (jsonlSidecarRecord, error) {
	description := fmt.Sprintf("sidecar line %d", line)
	var record jsonlSidecarRecord
	if err := json.Unmarshal(text, &record); err != nil {
		return record, &JsonlCorruptionError{Message: "Malformed complete " + description, Cause: err}
	}
	if record.Format != jsonlFormatVersion || record.Type != "record" || record.Seq < 1 || record.Ordinal < 0 {
		return record, &JsonlCorruptionError{Message: "Invalid sidecar record in " + description}
	}
	switch record.Payload.Type {
	case "task":
		var task TaskRecord
		if err := json.Unmarshal(record.Payload.Value, &task); err != nil || task.ID == 0 || task.State.Status == TaskTerminal {
			return record, &JsonlCorruptionError{Message: "Invalid live task record in " + description}
		}
	case "document":
		if record.Payload.ID == nil {
			return record, &JsonlCorruptionError{Message: "Invalid document record in " + description}
		}
		var content DocumentContent
		if err := json.Unmarshal(record.Payload.Content, &content); err != nil {
			return record, &JsonlCorruptionError{Message: "Invalid document content in " + description}
		}
		if content.Version < 1 || (content.Kind != ContentBase && content.Kind != ContentDelta) {
			return record, &JsonlCorruptionError{Message: "Invalid document content in " + description}
		}
		if content.Kind == ContentBase && len(content.Value) == 0 {
			return record, &JsonlCorruptionError{Message: "Invalid document content in " + description}
		}
		if content.Kind == ContentDelta && content.Ops == nil {
			return record, &JsonlCorruptionError{Message: "Invalid document content in " + description}
		}
	default:
		return record, &JsonlCorruptionError{Message: "Unknown sidecar record type in " + description}
	}
	return record, nil
}

func sortedSidecarNames(sidecars map[string]string) []string {
	names := make([]string, 0, len(sidecars))
	for name := range sidecars {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// appendFile appends (creating when missing).
func appendFile(path string, data []byte) error {
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

// flushFile fsyncs one file.
func flushFile(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	err = file.Sync()
	_ = file.Close()
	return err
}

// utf8Valid reports whether the bytes are valid UTF-8.
func utf8Valid(data []byte) bool {
	return utf8.Valid(data)
}
