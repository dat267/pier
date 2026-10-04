package durable

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"testing"
)

// Port of packages/durable/src/testing/storage-benchmark.ts: the deterministic
// representative dataset and the read/write benchmark descriptors. Benchmarks
// run only under -bench, so they never slow the plain gate.

// StorageBenchmarkScale is one dataset size.
type StorageBenchmarkScale struct {
	Name          string
	EntryCount    int
	TaskCount     int
	DocumentCount int
}

// TimingScale is the small scale used for timing benchmarks.
var TimingScale = StorageBenchmarkScale{Name: "timing", EntryCount: 1_000, TaskCount: 300, DocumentCount: 300}

var replayTails = []int{0, 16, 128, 1024}

const (
	historySegmentLength = 128
	forkDepth            = 8
	entriesPerFork       = 32
	benchmarkBatchSize   = 100
)

// StorageBenchmarkDataset is the deterministic dataset the read benchmarks use.
type StorageBenchmarkDataset struct {
	FirstEntryID         Id
	FilteredTaskCount    int
	ExactDocumentID      Id
	ExactDocumentKey     string
	ReplayDocumentIDs    map[int]Id
	HistoricalDocumentID Id
	AncientAt            Seq
	RecentAt             Seq
	DeepestConversation  Id
	AncestorHeadEntryID  Id
}

func benchmarkTask(id Id, index int) *TaskRecord {
	statuses := []string{TaskPending, TaskRunning, TaskTerminal}
	status := statuses[index%len(statuses)]
	kind := "benchmark.other"
	if index%4 == 0 {
		kind = "benchmark.filtered"
	}
	record := &TaskRecord{
		ID: id, ConversationID: RootConversationID, Kind: kind, Version: 1,
		Input:      json.RawMessage(`{"index":` + strconv.Itoa(index) + `}`),
		Background: index%5 == 0, AbortRequested: index%7 == 0,
	}
	if status == TaskTerminal {
		record.State = TaskState{Status: status, Outcome: &TaskOutcome{
			Status: OutcomeCompleted, Result: json.RawMessage(`{"index":` + strconv.Itoa(index) + `}`),
		}}
	} else {
		record.State = TaskState{Status: status, Checkpoint: json.RawMessage(
			`{"index":` + strconv.Itoa(index) + `,"payload":"` + strings.Repeat("x", 64) + `"}`,
		)}
	}
	return record
}

// SeedStorageBenchmark seeds the deterministic dataset through the public
// Storage contract.
func SeedStorageBenchmark(storage Storage, scale StorageBenchmarkScale) (StorageBenchmarkDataset, error) {
	ctx := context.Background()
	if _, err := storage.Commit(ctx, []StorageWrite{conversationWrite(RootConversationID)}); err != nil {
		return StorageBenchmarkDataset{}, err
	}
	dataset := StorageBenchmarkDataset{ReplayDocumentIDs: map[int]Id{}}
	for start := 0; start < scale.EntryCount; start += benchmarkBatchSize {
		writes := []StorageWrite{}
		for index := start; index < minInt(start+benchmarkBatchSize, scale.EntryCount); index++ {
			id, err := storage.MintID(ctx)
			if err != nil {
				return StorageBenchmarkDataset{}, err
			}
			if index == 0 {
				dataset.FirstEntryID = id
				dataset.AncestorHeadEntryID = id
			}
			entry := &EntryRecord{ID: id, ConversationID: RootConversationID, Kind: "benchmark.entry",
				Data: json.RawMessage(`{"index":` + strconv.Itoa(index) + `,"text":"entry-` + strconv.Itoa(index) + `-` + strings.Repeat("x", 96) + `"}`)}
			if index == 0 {
				head := id
				entry.Head = &head
			}
			writes = append(writes, StorageWrite{Type: "entry", Entry: entry})
		}
		if _, err := storage.Commit(ctx, writes); err != nil {
			return StorageBenchmarkDataset{}, err
		}
	}
	for start := 0; start < scale.TaskCount; start += benchmarkBatchSize {
		writes := []StorageWrite{}
		for index := start; index < minInt(start+benchmarkBatchSize, scale.TaskCount); index++ {
			id, err := storage.MintID(ctx)
			if err != nil {
				return StorageBenchmarkDataset{}, err
			}
			writes = append(writes, StorageWrite{Type: "task", Task: benchmarkTask(id, index)})
		}
		if _, err := storage.Commit(ctx, writes); err != nil {
			return StorageBenchmarkDataset{}, err
		}
	}
	for start := 0; start < scale.DocumentCount; start += benchmarkBatchSize {
		writes := []StorageWrite{}
		for index := start; index < minInt(start+benchmarkBatchSize, scale.DocumentCount); index++ {
			id, err := storage.MintID(ctx)
			if err != nil {
				return StorageBenchmarkDataset{}, err
			}
			dataset.ExactDocumentID = id
			key := "key-" + strconv.Itoa(index)
			writes = append(writes, StorageWrite{
				Type:           "document.create",
				DocumentCreate: &DocumentCreate{ID: id, Kind: "benchmark.family", Key: &key, Scope: DocumentScope{Kind: ScopeSession}},
				DocumentContent: &DocumentContent{Version: 1, Kind: ContentBase,
					Value: json.RawMessage(`{"index":` + strconv.Itoa(index) + `,"text":"` + strings.Repeat("x", 128) + `"}`)},
			})
		}
		if _, err := storage.Commit(ctx, writes); err != nil {
			return StorageBenchmarkDataset{}, err
		}
	}
	dataset.ExactDocumentKey = "key-" + strconv.Itoa(scale.DocumentCount-1)
	// Replay tails: a shared sequence of short documents.
	replayWrites := []StorageWrite{}
	for _, tail := range replayTails {
		id, err := storage.MintID(ctx)
		if err != nil {
			return StorageBenchmarkDataset{}, err
		}
		dataset.ReplayDocumentIDs[tail] = id
		key := strconv.Itoa(tail)
		history, fork := HistoryRewindable, ForkAsOf
		root := RootConversationID
		replayWrites = append(replayWrites, StorageWrite{
			Type: "document.create",
			DocumentCreate: &DocumentCreate{ID: id, Kind: "benchmark.replay", Key: &key,
				Scope: DocumentScope{Kind: ScopeConversation, ConversationID: &root}, History: &history, Fork: &fork},
			DocumentContent: &DocumentContent{Version: 1, Kind: ContentBase,
				Value: json.RawMessage(`{"count":0,"text":"` + strings.Repeat("x", 64) + `"}`)},
		})
	}
	if _, err := storage.Commit(ctx, replayWrites); err != nil {
		return StorageBenchmarkDataset{}, err
	}
	for count := 1; count <= replayTails[len(replayTails)-1]; count++ {
		writes := []StorageWrite{}
		for _, tail := range replayTails {
			if count > tail {
				continue
			}
			id := dataset.ReplayDocumentIDs[tail]
			writes = append(writes, StorageWrite{Type: "document.change", DocumentID: &id,
				DocumentContent: &DocumentContent{Version: 1, Kind: ContentDelta,
					Ops: []any{[]any{"s", []any{"count"}, count}}}})
		}
		if _, err := storage.Commit(ctx, writes); err != nil {
			return StorageBenchmarkDataset{}, err
		}
	}
	// A historical document: an ancient segment, a base, then a recent segment.
	historical, err := storage.MintID(ctx)
	if err != nil {
		return StorageBenchmarkDataset{}, err
	}
	dataset.HistoricalDocumentID = historical
	root := RootConversationID
	history, fork := HistoryRewindable, ForkAsOf
	if _, err := storage.Commit(ctx, []StorageWrite{{
		Type: "document.create",
		DocumentCreate: &DocumentCreate{ID: historical, Kind: "benchmark.history",
			Scope: DocumentScope{Kind: ScopeConversation, ConversationID: &root}, History: &history, Fork: &fork},
		DocumentContent: &DocumentContent{Version: 1, Kind: ContentBase, Value: json.RawMessage(`{"count":0}`)},
	}}); err != nil {
		return StorageBenchmarkDataset{}, err
	}
	for count := 1; count <= historySegmentLength; count++ {
		seq, err := storage.Commit(ctx, []StorageWrite{{Type: "document.change", DocumentID: &historical,
			DocumentContent: &DocumentContent{Version: 1, Kind: ContentDelta,
				Ops: []any{[]any{"s", []any{"count"}, count}}}}})
		if err != nil {
			return StorageBenchmarkDataset{}, err
		}
		dataset.AncientAt = seq
	}
	if _, err := storage.Commit(ctx, []StorageWrite{{Type: "document.change", DocumentID: &historical,
		DocumentContent: &DocumentContent{Version: 1, Kind: ContentBase,
			Value: json.RawMessage(`{"count":` + strconv.Itoa(historySegmentLength) + `}`)}}}); err != nil {
		return StorageBenchmarkDataset{}, err
	}
	dataset.RecentAt = dataset.AncientAt
	for count := historySegmentLength + 1; count <= historySegmentLength*2; count++ {
		seq, err := storage.Commit(ctx, []StorageWrite{{Type: "document.change", DocumentID: &historical,
			DocumentContent: &DocumentContent{Version: 1, Kind: ContentDelta,
				Ops: []any{[]any{"s", []any{"count"}, count}}}}})
		if err != nil {
			return StorageBenchmarkDataset{}, err
		}
		dataset.RecentAt = seq
	}
	// A chain of forked conversations.
	parentConversation := RootConversationID
	parentAt := dataset.FirstEntryID
	for depth := 0; depth < forkDepth; depth++ {
		conversation, err := storage.MintID(ctx)
		if err != nil {
			return StorageBenchmarkDataset{}, err
		}
		if _, err := storage.Commit(ctx, []StorageWrite{{Type: "conversation", Conversation: &ConversationRecord{
			ID: conversation, Parent: &ConversationParent{ConversationID: parentConversation, At: parentAt},
		}}}); err != nil {
			return StorageBenchmarkDataset{}, err
		}
		ids := make([]Id, entriesPerFork)
		for index := range ids {
			minted, err := storage.MintID(ctx)
			if err != nil {
				return StorageBenchmarkDataset{}, err
			}
			ids[index] = minted
		}
		writes := make([]StorageWrite, 0, len(ids))
		for index, id := range ids {
			writes = append(writes, StorageWrite{Type: "entry", Entry: &EntryRecord{
				ID: id, ConversationID: conversation, Kind: "benchmark.fork",
				Data: json.RawMessage(`{"depth":` + strconv.Itoa(depth) + `,"index":` + strconv.Itoa(index) + `}`),
			}})
		}
		if _, err := storage.Commit(ctx, writes); err != nil {
			return StorageBenchmarkDataset{}, err
		}
		parentConversation = conversation
		parentAt = ids[len(ids)-1]
		dataset.DeepestConversation = conversation
	}
	dataset.FilteredTaskCount = minInt(50, (scale.TaskCount+59)/60)
	// The root's first entry (head self) is the marker visible through every
	// fork, so it is the head the deepest conversation resolves.
	dataset.AncestorHeadEntryID = dataset.FirstEntryID
	return dataset, nil
}

// BenchmarkStorageReads runs the read benchmark descriptors.
func BenchmarkStorageReads(b *testing.B) {
	storage := NewMemoryStorage()
	dataset, err := SeedStorageBenchmark(storage, TimingScale)
	if err != nil {
		b.Fatal(err)
	}
	ctx := context.Background()
	benchmarks := []struct {
		name     string
		expected int
		run      func() (int, error)
	}{
		{"exact entry lookup", int(dataset.FirstEntryID), func() (int, error) {
			commit, err := storage.Entry(ctx, dataset.FirstEntryID)
			if err != nil || commit == nil {
				return -1, err
			}
			return int(commit.Entry.ID), nil
		}},
		{"entry page scan (100)", 100, func() (int, error) {
			page, err := storage.ScanEntries(ctx, EntryQuery{ConversationID: RootConversationID}, nil, 100)
			return len(page.Items), err
		}},
		{"filtered task scan (50)", dataset.FilteredTaskCount, func() (int, error) {
			kind, status, background := "benchmark.filtered", TaskPending, true
			page, err := storage.ScanTasks(ctx, TaskQuery{Kind: &kind, Status: &status, Background: &background}, nil, 50)
			return len(page.Items), err
		}},
		{"exact document address", int(dataset.ExactDocumentID), func() (int, error) {
			found, err := storage.FindDocument(ctx, DocumentAddress{Kind: "benchmark.family",
				Key: &dataset.ExactDocumentKey, Scope: DocumentScope{Kind: ScopeSession}}, CurrentDocumentPoint())
			if err != nil || found == nil {
				return -1, err
			}
			return int(found.ID), nil
		}},
		{"fork-depth history scan (100)", 100, func() (int, error) {
			page, err := storage.ScanEntries(ctx, EntryQuery{ConversationID: dataset.DeepestConversation}, nil, 100)
			return len(page.Items), err
		}},
	}
	for _, benchmark := range benchmarks {
		benchmark := benchmark
		b.Run(benchmark.name, func(b *testing.B) {
			for iteration := 0; iteration < b.N; iteration++ {
				got, err := benchmark.run()
				if err != nil {
					b.Fatal(err)
				}
				if got != benchmark.expected {
					b.Fatalf("got %d, want %d", got, benchmark.expected)
				}
			}
		})
	}
	for _, tail := range replayTails {
		tail := tail
		b.Run("document replay tail ("+strconv.Itoa(tail)+")", func(b *testing.B) {
			for iteration := 0; iteration < b.N; iteration++ {
				stored, err := storage.Document(ctx, dataset.ReplayDocumentIDs[tail], CurrentDocumentPoint())
				if err != nil || stored == nil {
					b.Fatal(err)
				}
				var value struct {
					Count int `json:"count"`
				}
				if err := json.Unmarshal(stored.Value, &value); err != nil || value.Count != tail {
					b.Fatalf("count = %d, want %d", value.Count, tail)
				}
			}
		})
	}
	b.Run("ancient historical read", func(b *testing.B) {
		for iteration := 0; iteration < b.N; iteration++ {
			stored, err := storage.Document(ctx, dataset.HistoricalDocumentID, DocumentPointAt(dataset.AncientAt))
			if err != nil || stored == nil {
				b.Fatal(err)
			}
			var value struct {
				Count int `json:"count"`
			}
			_ = json.Unmarshal(stored.Value, &value)
			if value.Count != historySegmentLength {
				b.Fatalf("count = %d", value.Count)
			}
		}
	})
	b.Run("fork-depth head lookup", func(b *testing.B) {
		for iteration := 0; iteration < b.N; iteration++ {
			marker, err := storage.FindLatestHeadMarker(ctx, dataset.DeepestConversation, nil)
			if err != nil || marker == nil || marker.ID != dataset.AncestorHeadEntryID {
				b.Fatalf("marker = %+v, %v", marker, err)
			}
		}
	})
}

// SeedStorageWriteBenchmark seeds the common state the write benchmarks use.
func SeedStorageWriteBenchmark(storage Storage) error {
	ctx := context.Background()
	if _, err := storage.Commit(ctx, []StorageWrite{conversationWrite(RootConversationID)}); err != nil {
		return err
	}
	writes := make([]StorageWrite, 0, 100)
	for index := 0; index < 100; index++ {
		id, err := storage.MintID(ctx)
		if err != nil {
			return err
		}
		writes = append(writes, StorageWrite{Type: "entry", Entry: &EntryRecord{
			ID: id, ConversationID: RootConversationID, Kind: "benchmark.baseline",
			Data: json.RawMessage(`{"index":` + strconv.Itoa(index) + `}`),
		}})
	}
	_, err := storage.Commit(ctx, writes)
	return err
}

// BenchmarkStorageWrites runs the write benchmark descriptors.
func BenchmarkStorageWrites(b *testing.B) {
	ctx := context.Background()
	b.Run("commit one entry", func(b *testing.B) {
		storage := NewMemoryStorage()
		if err := SeedStorageWriteBenchmark(storage); err != nil {
			b.Fatal(err)
		}
		for iteration := 0; iteration < b.N; iteration++ {
			id, err := storage.MintID(ctx)
			if err != nil {
				b.Fatal(err)
			}
			if _, err := storage.Commit(ctx, []StorageWrite{{Type: "entry", Entry: &EntryRecord{
				ID: id, ConversationID: RootConversationID, Kind: "benchmark.write",
				Data: json.RawMessage(`{"text":"` + strings.Repeat("x", 128) + `"}`),
			}}}); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("commit 100 entries", func(b *testing.B) {
		storage := NewMemoryStorage()
		if err := SeedStorageWriteBenchmark(storage); err != nil {
			b.Fatal(err)
		}
		for iteration := 0; iteration < b.N; iteration++ {
			writes := make([]StorageWrite, 0, 100)
			for index := 0; index < 100; index++ {
				id, err := storage.MintID(ctx)
				if err != nil {
					b.Fatal(err)
				}
				writes = append(writes, StorageWrite{Type: "entry", Entry: &EntryRecord{
					ID: id, ConversationID: RootConversationID, Kind: "benchmark.write",
					Data: json.RawMessage(`{"index":` + strconv.Itoa(index) + `,"text":"` + strings.Repeat("x", 128) + `"}`),
				}})
			}
			if _, err := storage.Commit(ctx, writes); err != nil {
				b.Fatal(err)
			}
		}
	})
}
