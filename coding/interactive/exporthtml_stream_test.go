package interactive

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dat267/pier/ai"
	"github.com/dat267/pier/coding"
)

// The streamed payload must be the same document member for member as the
// in-memory marshal: the export changes shape only in how it is produced (D489).
func TestStreamedExportPayloadMatchesTheDocument(t *testing.T) {
	SetCustomThemesDir(t.TempDir())
	SetRegisteredThemes(nil)
	InitTheme("dark", false)

	prompt := "system prompt"
	tools := []exportTool{{Name: "read", Description: "Read", Parameters: []byte(`{"type":"object"}`)}}
	entries := []coding.SessionEntry{
		{Type: "message", Message: []byte(`{"role":"user","content":"hi"}`)},
		{Type: "compaction", Summary: "summary", FirstKeptEntryID: "m1"},
	}
	data := &exportSessionData{
		Header:       []byte(`{"type":"session","id":"s1"}`),
		Entries:      entries,
		LeafID:       new(string),
		SystemPrompt: &prompt,
		Tools:        &tools,
	}
	*data.LeafID = "m1"

	want := GenerateSessionHTML(data, "dark")

	var builder bytes.Buffer
	if err := WriteSessionHTML(&builder, data, nil, "dark"); err != nil {
		t.Fatal(err)
	}
	if got := builder.String(); got != want {
		t.Fatal("streamed document differs from the in-memory one")
	}

	// And feeding the entries through an iterator, as an export of a loaded
	// session does, must not change a byte either.
	var streamed bytes.Buffer
	each := func(yield func(coding.SessionEntry) error) error {
		for _, entry := range entries {
			if err := yield(entry); err != nil {
				return err
			}
		}
		return nil
	}
	if err := WriteSessionHTML(&streamed, data, each, "dark"); err != nil {
		t.Fatal(err)
	}
	if streamed.String() != want {
		t.Fatal("iterator-fed document differs from the in-memory one")
	}

	// The payload is base64 of the document JSON, so the streaming write must
	// still decode to the same members.
	extract := func(html string) string {
		start := strings.Index(html, `<script id="session-data" type="application/json">`)
		start += len(`<script id="session-data" type="application/json">`)
		end := strings.Index(html[start:], `</script>`)
		decoded, err := base64.StdEncoding.DecodeString(html[start : start+end])
		if err != nil {
			t.Fatal(err)
		}
		return string(decoded)
	}
	if got, want := extract(streamed.String()), extract(want); got != want {
		t.Fatalf("payload differs:\n got %s\nwant %s", got, want)
	}
}

// ExportFromFile streams a reopened session, where every entry carries its
// retained line. A custom entry serializes differently from its struct form
// (CR-073), so this pins the streamed document against the in-memory one: the
// line, not a re-encode, must reach the export.
func TestExportFromFileMatchesTheInMemoryDocument(t *testing.T) {
	persist := true
	source := coding.NewSessionManager(t.TempDir(), &coding.SessionManagerOptions{Persist: &persist})
	source.AppendMessage(&ai.UserMessage{Content: ai.StringOrBlocks{Text: "hello"}})
	source.AppendCustomEntry("pi-goal", json.RawMessage(`{"operation":"clear","cleared":{"id":"g1"}}`))
	source.AppendCompaction("summary", "", 10, nil, false, nil)
	path := source.GetSessionFile()

	reopened, err := coding.OpenSession(path, "", "")
	if err != nil {
		t.Fatal(err)
	}
	data := &exportSessionData{
		Header:  exportHeaderJSON(reopened.GetHeader()),
		Entries: reopened.GetEntries(),
		LeafID:  reopened.GetLeafID(),
	}
	want := GenerateSessionHTML(data, "dark")

	outputPath := filepath.Join(t.TempDir(), "export.html")
	if _, err := ExportFromFile(path, outputPath, "dark"); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Fatal("ExportFromFile differs from the in-memory document")
	}
}
