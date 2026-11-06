package coding

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/dat267/pier/ai"
)

// D379: summary file lists are capped so the tracked paths cannot grow without
// bound across compactions. Upstream accumulates the previous compaction's
// details plus the current window and writes every path into the summary; on a
// long session that list reached thousands of lines and most of the summary's
// bytes. The cap keeps the most recently seen paths and reports how many were
// left out.
func TestSummaryFileListsCapToTheMostRecentPaths(t *testing.T) {
	fileOps := CreateFileOps()
	for i := 0; i < maxSummaryFileListEntries+50; i++ {
		fileOps.markRead(fmt.Sprintf("read-%03d", i))
		fileOps.markEdited(fmt.Sprintf("edit-%03d", i))
	}

	readFiles, modifiedFiles, readOmitted, modifiedOmitted := CapFileLists(fileOps)
	if len(readFiles) != maxSummaryFileListEntries || readOmitted != 50 {
		t.Fatalf("read list = %d files, omitted %d; want %d and 50", len(readFiles), readOmitted, maxSummaryFileListEntries)
	}
	if len(modifiedFiles) != maxSummaryFileListEntries || modifiedOmitted != 50 {
		t.Fatalf("modified list = %d files, omitted %d; want %d and 50", len(modifiedFiles), modifiedOmitted, maxSummaryFileListEntries)
	}
	// The most recent entries survive and the output stays sorted.
	if want := fmt.Sprintf("read-%03d", 50); readFiles[0] != want {
		t.Fatalf("first read file = %q, want %q", readFiles[0], want)
	}
	if readFiles[len(readFiles)-1] != fmt.Sprintf("read-%03d", maxSummaryFileListEntries+49) {
		t.Fatalf("last read file = %q", readFiles[len(readFiles)-1])
	}
	if modifiedFiles[0] != "edit-050" {
		t.Fatalf("first modified file = %q, want edit-050", modifiedFiles[0])
	}
	if !sortedAscending(readFiles) || !sortedAscending(modifiedFiles) {
		t.Fatalf("lists are not sorted: %v / %v", readFiles, modifiedFiles)
	}
}

func TestSummaryFileListsPreferTheCurrentWindow(t *testing.T) {
	fileOps := CreateFileOps()
	// Inherited details are marked first, then the window: the cap keeps the
	// window's paths even when the inherited list is already at the cap.
	for i := 0; i < maxSummaryFileListEntries; i++ {
		fileOps.markRead(fmt.Sprintf("old-%03d", i))
	}
	for i := 0; i < 10; i++ {
		fileOps.markRead(fmt.Sprintf("new-%03d", i))
	}
	readFiles, _, omitted, _ := CapFileLists(fileOps)
	if omitted != 10 {
		t.Fatalf("omitted = %d, want 10", omitted)
	}
	for i := 0; i < 10; i++ {
		want := fmt.Sprintf("new-%03d", i)
		if !containsFoldExact(readFiles, want) {
			t.Fatalf("window path %q was capped away: %v", want, readFiles)
		}
	}
}

func TestSummaryFileListsSmallInputMatchesUpstreamFormatting(t *testing.T) {
	fileOps := CreateFileOps()
	fileOps.markRead("a.txt")
	fileOps.markRead("b.txt")
	fileOps.markEdited("b.txt")
	fileOps.markWritten("c.txt")

	readFiles, modifiedFiles, readOmitted, modifiedOmitted := CapFileLists(fileOps)
	if readOmitted != 0 || modifiedOmitted != 0 {
		t.Fatalf("omitted = %d/%d, want none", readOmitted, modifiedOmitted)
	}
	// Capping must not perturb the upstream rendering when nothing is dropped.
	if got, want := FormatSummaryFileOperations(readFiles, modifiedFiles, 0, 0), FormatFileOperations(readFiles, modifiedFiles); got != want {
		t.Fatalf("formatted = %q, want the upstream shape %q", got, want)
	}
}

func TestSummaryFileListsReportOmittedCounts(t *testing.T) {
	fileOps := CreateFileOps()
	for i := 0; i < maxSummaryFileListEntries+7; i++ {
		fileOps.markRead(fmt.Sprintf("f-%03d", i))
	}
	readFiles, modifiedFiles, readOmitted, modifiedOmitted := CapFileLists(fileOps)
	formatted := FormatSummaryFileOperations(readFiles, modifiedFiles, readOmitted, modifiedOmitted)
	if !strings.Contains(formatted, "<read-files>\n") || !strings.Contains(formatted, "\n+7 more\n</read-files>") {
		t.Fatalf("formatted = %q, want a +7 more marker inside the read block", formatted)
	}
	if strings.Contains(formatted, "<modified-files>") {
		t.Fatalf("formatted = %q, want no empty modified block", formatted)
	}
}

func containsFoldExact(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func sortedAscending(values []string) bool {
	for i := 1; i < len(values); i++ {
		if values[i-1] > values[i] {
			return false
		}
	}
	return true
}

// The cap has to reach the produced summary and the persisted details, not just
// the helper: details are what the next compaction inherits, so an uncapped
// details list would keep growing even with a capped summary.
func TestCompactSummaryCapsTheFileLists(t *testing.T) {
	fileOps := CreateFileOps()
	for i := 0; i < maxSummaryFileListEntries+25; i++ {
		fileOps.markRead(fmt.Sprintf("read-%03d", i))
		fileOps.markEdited(fmt.Sprintf("edit-%03d", i))
	}
	preparation := &CompactionPreparation{
		MessagesToSummarize: []ai.Message{&ai.UserMessage{Content: ai.StringOrBlocks{Text: "summarize me"}}},
		FirstKeptEntryID:    "kept",
		TokensBefore:        1000,
		FileOps:             fileOps,
		Settings:            DefaultCompactionSettings,
	}
	options := CompactionOptions{
		Model: &ai.Model{ID: "mock", API: "openai-responses", Provider: "openai", MaxTokens: 8192},
		StreamFn: func(model *ai.Model, context ai.TranscriptContext, options *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
			stream := ai.NewAssistantMessageEventStream()
			stream.Push(ai.AssistantMessageEvent{Type: ai.EventDone, Reason: ai.StopStop, Message: &ai.AssistantMessage{
				Content: ai.ContentList{ai.TextContent{Text: "## Goal\ntest summary long enough to pass the usability check"}},
				API:     model.API, Provider: model.Provider, Model: model.ID, StopReason: ai.StopStop, Timestamp: 1,
			}})
			return stream
		},
	}
	result, err := Compact(preparation, options)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Summary, "+25 more\n</read-files>") {
		t.Fatalf("summary does not report the omitted reads: %q", result.Summary)
	}
	if !strings.Contains(result.Summary, "+25 more\n</modified-files>") {
		t.Fatalf("summary does not report the omitted edits: %q", result.Summary)
	}
	var details CompactionDetails
	if err := json.Unmarshal(result.Details, &details); err != nil {
		t.Fatal(err)
	}
	if len(details.ReadFiles) != maxSummaryFileListEntries || len(details.ModifiedFiles) != maxSummaryFileListEntries {
		t.Fatalf("details = %d read, %d modified; want %d each", len(details.ReadFiles), len(details.ModifiedFiles), maxSummaryFileListEntries)
	}
}

// At exactly the cap nothing is omitted, so the summary must still be the
// upstream bytes with no marker: an off-by-one here would add a "+0 more" line
// that upstream never writes.
func TestSummaryFileListsAtTheCapMatchUpstream(t *testing.T) {
	fileOps := CreateFileOps()
	readPaths := make([]string, 0, maxSummaryFileListEntries)
	for i := 0; i < maxSummaryFileListEntries; i++ {
		path := fmt.Sprintf("read-%03d", i)
		readPaths = append(readPaths, path)
		fileOps.markRead(path)
	}
	readFiles, modifiedFiles, readOmitted, modifiedOmitted := CapFileLists(fileOps)
	if readOmitted != 0 || modifiedOmitted != 0 {
		t.Fatalf("omitted = %d/%d, want 0/0 at the cap", readOmitted, modifiedOmitted)
	}
	if got, want := FormatSummaryFileOperations(readFiles, modifiedFiles, readOmitted, modifiedOmitted), FormatFileOperations(readPaths, nil); got != want {
		t.Errorf("summary = %q, want the upstream bytes %q", got, want)
	}
}
