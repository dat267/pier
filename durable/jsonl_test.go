package durable

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestJsonlLogFormat pins the on-disk format: one commit marker per commit,
// the port's `input` write as the upstream `submission` operation, and a live
// task in a task-<id>.jsonl sidecar (upstream storage/jsonl).
func TestJsonlLogFormat(t *testing.T) {
	dir := t.TempDir()
	storage, err := OpenJsonlStorage(context.Background(), dir, JsonlStorageOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = storage.Close(context.Background()) }()

	mustCommit(t, storage, conversationWrite(2))
	mustCommit(t, storage, StorageWrite{Type: "submission", Submission: &SubmissionRecord{
		ID: 3, ConversationID: 2, Type: SubmissionTypeInput, RequestID: strPtr("req-1"), Status: SubmissionQueued,
	}})
	mustCommit(t, storage, StorageWrite{Type: "task", Task: &TaskRecord{
		ID: 4, ConversationID: 2, Kind: "t", State: TaskState{Status: TaskRunning},
	}})
	// The live task is in a sidecar while running...
	sidecar, err := os.ReadFile(filepath.Join(dir, "task-4.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(sidecar), `"type":"record"`) || !strings.Contains(string(sidecar), `"status":"running"`) {
		t.Fatalf("sidecar = %s", sidecar)
	}
	mustCommit(t, storage, StorageWrite{Type: "task", Task: &TaskRecord{
		ID: 4, ConversationID: 2, Kind: "t", State: TaskState{Status: TaskTerminal},
	}})

	main, err := os.ReadFile(filepath.Join(dir, "main.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(string(main), "\n"), "\n")
	if len(lines) != 4 {
		t.Fatalf("markers = %d: %s", len(lines), main)
	}
	if !strings.Contains(lines[0], `"format":1`) || !strings.Contains(lines[0], `"type":"commit"`) ||
		!strings.Contains(lines[0], `"seq":1`) {
		t.Fatalf("marker 1 = %s", lines[0])
	}
	if !strings.Contains(lines[1], `"type":"submission"`) {
		t.Fatalf("submission marker = %s", lines[1])
	}
	if !strings.Contains(lines[2], `"type":"task.sidecar"`) || !strings.Contains(lines[2], `"ordinal":0`) {
		t.Fatalf("live task marker = %s", lines[2])
	}
	// ...and terminal in the marker, with the sidecar reclaimed.
	if !strings.Contains(lines[3], `"status":"terminal"`) || !strings.Contains(lines[3], `"type":"task"`) {
		t.Fatalf("terminal task marker = %s", lines[3])
	}
	if _, err := os.Stat(filepath.Join(dir, "task-4.jsonl")); !os.IsNotExist(err) {
		t.Fatalf("sidecar not reclaimed: %v", err)
	}
}

// TestJsonlReopenReplaysCommits covers recovery: a reopened storage serves the
// committed state from its in-memory mirror.
func TestJsonlReopenReplaysCommits(t *testing.T) {
	dir := t.TempDir()
	storage, err := OpenJsonlStorage(context.Background(), dir, JsonlStorageOptions{})
	if err != nil {
		t.Fatal(err)
	}
	mustCommit(t, storage, conversationWrite(2))
	mustCommit(t, storage, entryWrite(10, 2, idPtr(10)))
	mustCommit(t, storage, StorageWrite{Type: "task", Task: &TaskRecord{
		ID: 4, ConversationID: 2, Kind: "t", State: TaskState{Status: TaskRunning},
	}})
	mustCommit(t, storage, StorageWrite{Type: "submission", Submission: &SubmissionRecord{
		ID: 5, ConversationID: 2, Type: SubmissionTypeInput, RequestID: strPtr("req-1"), Status: SubmissionPlaced, Entry: idPtr(10),
	}})
	if err := storage.Close(context.Background()); err != nil {
		t.Fatal(err)
	}

	reopened, err := OpenJsonlStorage(context.Background(), dir, JsonlStorageOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = reopened.Close(context.Background()) }()
	conversation, err := reopened.Conversation(context.Background(), 2)
	if err != nil || conversation == nil || conversation.ID != 2 {
		t.Fatalf("conversation = %+v err=%v", conversation, err)
	}
	entry, err := reopened.Entry(context.Background(), 10)
	if err != nil || entry == nil || entry.CommitSeq != 2 {
		t.Fatalf("entry = %+v err=%v", entry, err)
	}
	task, err := reopened.Task(context.Background(), 4)
	if err != nil || task == nil || task.State.Status != TaskRunning {
		t.Fatalf("task = %+v err=%v", task, err)
	}
	submission, err := reopened.SubmissionByRequest(context.Background(), 2, "req-1")
	if err != nil || submission == nil || submission.ID != 5 || submission.Entry == nil || *submission.Entry != 10 {
		t.Fatalf("submission = %+v err=%v", submission, err)
	}
	// A new commit continues the sequence.
	if seq := mustCommit(t, reopened, conversationWrite(20)); seq != 5 {
		t.Fatalf("seq = %d", seq)
	}
}

// TestJsonlTruncatesTornTail covers the recovery of a partially written last
// line (upstream readLines).
func TestJsonlTruncatesTornTail(t *testing.T) {
	dir := t.TempDir()
	storage, err := OpenJsonlStorage(context.Background(), dir, JsonlStorageOptions{})
	if err != nil {
		t.Fatal(err)
	}
	mustCommit(t, storage, conversationWrite(2))
	if err := storage.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "main.jsonl")
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString(`{"format":1,"type":"commit","seq":2,"wri`); err != nil {
		t.Fatal(err)
	}
	_ = file.Close()

	reopened, err := OpenJsonlStorage(context.Background(), dir, JsonlStorageOptions{})
	if err != nil {
		t.Fatalf("torn tail not recovered: %v", err)
	}
	defer func() { _ = reopened.Close(context.Background()) }()
	if seq := mustCommit(t, reopened, conversationWrite(3)); seq != 2 {
		t.Fatalf("seq = %d", seq)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	// A torn tail would make the second line invalid JSON.
	if len(lines) != 2 {
		t.Fatalf("torn tail not truncated: %s", data)
	}
	for index, line := range lines {
		var marker struct {
			Seq int `json:"seq"`
		}
		if err := json.Unmarshal([]byte(line), &marker); err != nil || marker.Seq != index+1 {
			t.Fatalf("line %d = %s (err %v)", index, line, err)
		}
	}
}

// TestJsonlRejectsCorruption covers the corruption error paths.
func TestJsonlRejectsCorruption(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.jsonl"), []byte("{\"format\":1,\"type\":\"commit\",\"seq\":1,\"writes\":[{\"type\":\"nope\"}]}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenJsonlStorage(context.Background(), dir, JsonlStorageOptions{}); err == nil ||
		!strings.Contains(err.Error(), "Unknown write type") {
		t.Fatalf("err = %v", err)
	}

	// A sequence that does not strictly increase.
	dir = t.TempDir()
	marker := "{\"format\":1,\"type\":\"commit\",\"seq\":1,\"writes\":[]}\n"
	if err := os.WriteFile(filepath.Join(dir, "main.jsonl"), []byte(marker+marker), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenJsonlStorage(context.Background(), dir, JsonlStorageOptions{}); err == nil ||
		!strings.Contains(err.Error(), "strictly increase") {
		t.Fatalf("err = %v", err)
	}

	// A document marker whose replay fails (no such document) is reported as
	// invalid committed state.
	dir = t.TempDir()
	document := "{\"format\":1,\"type\":\"commit\",\"seq\":1,\"writes\":[{\"type\":\"document.retire\",\"id\":7}]}\n"
	if err := os.WriteFile(filepath.Join(dir, "main.jsonl"), []byte(document), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenJsonlStorage(context.Background(), dir, JsonlStorageOptions{}); err == nil ||
		!strings.Contains(err.Error(), "Invalid committed state") {
		t.Fatalf("err = %v", err)
	}

	// A document creation without a record id is a malformed marker.
	dir = t.TempDir()
	badCreate := "{\"format\":1,\"type\":\"commit\",\"seq\":1,\"writes\":[{\"type\":\"document.create\",\"record\":{\"kind\":\"k\"},\"ordinal\":0}]}\n"
	if err := os.WriteFile(filepath.Join(dir, "main.jsonl"), []byte(badCreate), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenJsonlStorage(context.Background(), dir, JsonlStorageOptions{}); err == nil ||
		!strings.Contains(err.Error(), "Invalid document creation") {
		t.Fatalf("err = %v", err)
	}
}

// TestJsonlPoisonsAfterAppendFailure covers the poison semantics: once an
// append fails, the storage refuses further use until reopened.
func TestJsonlPoisonsAfterAppendFailure(t *testing.T) {
	dir := t.TempDir()
	storage, err := OpenJsonlStorage(context.Background(), dir, JsonlStorageOptions{})
	if err != nil {
		t.Fatal(err)
	}
	// Make the directory unwritable so the marker append fails.
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Skipf("cannot chmod: %v", err)
	}
	_, commitErr := storage.Commit(context.Background(), []StorageWrite{conversationWrite(2)})
	if commitErr == nil {
		_ = os.Chmod(dir, 0o755)
		t.Skip("the append unexpectedly succeeded (running as root?)")
	}
	if !strings.Contains(commitErr.Error(), "poisoned") {
		t.Fatalf("err = %v", commitErr)
	}
	if _, err := storage.Conversation(context.Background(), 2); err == nil || !strings.Contains(err.Error(), "poisoned") {
		t.Fatalf("read after poison = %v", err)
	}
	_ = os.Chmod(dir, 0o755)
}
