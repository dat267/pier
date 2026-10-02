package durable

import (
	"encoding/json"
	"strings"
	"testing"
)

// Port of entries.ts and tasks.ts.

func TestDefineEntryValidatesKind(t *testing.T) {
	if _, err := DefineEntry(""); err == nil || !strings.Contains(err.Error(), "non-empty string") {
		t.Fatalf("err = %v", err)
	}
	token, err := DefineEntry("custom")
	if err != nil || token.Kind != "custom" {
		t.Fatalf("token = %+v err=%v", token, err)
	}
	if !token.Matches(&EntryRecord{Kind: "custom"}) {
		t.Fatal("a matching record must narrow")
	}
	if token.Matches(&EntryRecord{Kind: "other"}) || token.Matches(nil) {
		t.Fatal("a non-matching record must not narrow")
	}
	// The built-in kinds are the upstream constants.
	if UserEntry.Kind != "pi.user" || AssistantEntry.Kind != "pi.assistant" ||
		SystemEntry.Kind != "pi.system" || ToolResultEntry.Kind != "pi.tool-result" ||
		ResetEntry.Kind != "pi.reset" || CompactionEntry.Kind != "pi.compaction" {
		t.Fatal("built-in entry kinds changed")
	}
}

func TestEntryDataOf(t *testing.T) {
	record := &EntryRecord{Kind: "pi.tool-result", Data: json.RawMessage(`{"diagnostics":[{"severity":"warn","message":"m","code":"c"}]}`)}
	value, ok := EntryDataOf(ToolResultEntry, record)
	if !ok {
		t.Fatal("data missing")
	}
	diagnostics := value.(map[string]any)["diagnostics"].([]any)
	if len(diagnostics) != 1 {
		t.Fatalf("diagnostics = %+v", diagnostics)
	}
	if _, ok := EntryDataOf(UserEntry, record); ok {
		t.Fatal("a different kind must not match")
	}
}

func TestToolDiagnosticJSONShape(t *testing.T) {
	code := "c1"
	encoded, err := marshalJSONValue(ToolDiagnostic{Severity: DiagnosticWarn, Message: "m", Code: &code})
	if err != nil {
		t.Fatal(err)
	}
	if encoded != `{"severity":"warn","message":"m","code":"c1"}` {
		t.Fatalf("json = %s", encoded)
	}
}

func TestDefineTask(t *testing.T) {
	task := DefineTask(TaskDefinition{Name: "t", Version: 3})
	if task.Definition.Name != "t" || task.Definition.Version != 3 {
		t.Fatalf("task = %+v", task)
	}
}
