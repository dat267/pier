package durable

import (
	"context"
	"errors"
	"testing"

	"github.com/dat267/pier/ai"
	"github.com/dat267/pier/chord"
)

// Port of harness/prompt.ts.

func orderedStrings(pairs ...string) *orderedMap[string] {
	values := newOrderedMap[string]()
	for index := 0; index+1 < len(pairs); index += 2 {
		values.Set(pairs[index], pairs[index+1])
	}
	return values
}

func systemMessage(sections ...string) *ai.SystemMessage {
	message := &ai.SystemMessage{Content: ai.StringOrBlocks{Text: ""}}
	for index := 0; index+1 < len(sections); index += 2 {
		key, value := sections[index], sections[index+1]
		if value == "\x00" {
			message.SetSection(key, nil)
			continue
		}
		message.SetSection(key, stringPointer(value))
	}
	return message
}

func TestReplaySections(t *testing.T) {
	messages := []ai.Message{
		systemMessage("a", "1", "b", "2"),
		systemMessage("b", "\x00", "c", "3"),
		systemMessage("a", "4"),
	}
	shown := ReplaySections(messages)
	if keys := shown.Keys(); len(keys) != 2 || keys[0] != "a" || keys[1] != "c" {
		t.Fatalf("shown = %+v", keys)
	}
	if value, _ := shown.Get("a"); value != "4" {
		t.Fatalf("a = %q", value)
	}
}

func TestRenderSections(t *testing.T) {
	sections := []PromptSection{
		{Key: "a", Render: func(PromptInput, chord.Context) (string, bool, error) { return "A", true, nil }},
		{Key: "b", Tag: boolPointer(false), Render: func(PromptInput, chord.Context) (string, bool, error) { return "B", true, nil }},
		{Key: "c", Render: func(PromptInput, chord.Context) (string, bool, error) { return "", false, nil }},
	}
	reported := []error{}
	desired, err := RenderSections(sections, PromptInput{}, newOrderedMap[string](), func(e error) { reported = append(reported, e) }, context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if keys := desired.Keys(); len(keys) != 2 || keys[0] != "a" || keys[1] != "b" {
		t.Fatalf("desired = %+v", keys)
	}
	if value, _ := desired.Get("a"); value != "<a>\nA\n</a>" {
		t.Fatalf("a = %q", value)
	}
	if value, _ := desired.Get("b"); value != "B" {
		t.Fatalf("b = %q", value)
	}
	// A failing section keeps its shown text and is reported.
	failing := []PromptSection{{Key: "d", Render: func(PromptInput, chord.Context) (string, bool, error) {
		return "", false, errors.New("boom")
	}}}
	shown := orderedStrings("d", "kept")
	reported = nil
	desired, err = RenderSections(failing, PromptInput{}, shown, func(e error) { reported = append(reported, e) }, context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if value, _ := desired.Get("d"); value != "kept" {
		t.Fatalf("d = %q", value)
	}
	if len(reported) != 1 {
		t.Fatalf("reported = %+v", reported)
	}
	// After cancellation the failure propagates.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := RenderSections(failing, PromptInput{}, shown, func(error) {}, ctx); err == nil {
		t.Fatal("a cancelled context must propagate")
	}
}

func boolPointer(value bool) *bool { return &value }

func TestPlanSystemEntriesBaseline(t *testing.T) {
	head := EntryRecord{ID: 5, Kind: "pi.reset"}
	system := EntryRecord{ID: 3, Kind: SystemEntry.Kind}
	view := ContextView{Head: &head, Entries: []EntryRecord{head, system}}
	desired := orderedStrings("instructions", "hi")
	tools := []ai.Tool{{Name: "x", Description: "d"}}
	drafts := PlanSystemEntries(view, desired, tools, 42)
	if len(drafts) != 1 {
		t.Fatalf("drafts = %+v", drafts)
	}
	if drafts[0].Kind != SystemEntry.Kind || len(drafts[0].Edits) != 1 || drafts[0].Edits[0].Target != 3 ||
		drafts[0].Edits[0].Action != EditOmit {
		t.Fatalf("draft = %+v", drafts[0])
	}
	message, ok := drafts[0].Model[0].(*ai.SystemMessage)
	if !ok || message.Timestamp != 42 || len(message.ToolsAdded) != 1 || message.ToolsAdded[0].Name != "x" {
		t.Fatalf("message = %+v", drafts[0].Model[0])
	}
	if message.Sections["instructions"] == nil || *message.Sections["instructions"] != "hi" {
		t.Fatalf("sections = %+v", message.Sections)
	}
	// A system entry after the head marker disables the baseline.
	later := EntryRecord{ID: 6, Kind: SystemEntry.Kind}
	view.Entries = append(view.Entries, later)
	drafts = PlanSystemEntries(view, desired, tools, 42)
	if len(drafts) != 1 || len(drafts[0].Edits) != 0 {
		t.Fatalf("drafts = %+v", drafts)
	}
}

func TestPlanSystemEntriesMinimalPatch(t *testing.T) {
	message := &ai.SystemMessage{Content: ai.StringOrBlocks{Text: ""}}
	message.SetSection("a", stringPointer("1"))
	message.ToolsAdded = []ai.Tool{{Name: "x", Description: "d"}}
	view := ContextView{Messages: []ai.Message{message}}
	desired := orderedStrings("a", "2", "b", "3")
	drafts := PlanSystemEntries(view, desired, []ai.Tool{{Name: "x", Description: "d"}}, 7)
	if len(drafts) != 1 {
		t.Fatalf("drafts = %+v", drafts)
	}
	patch, ok := drafts[0].Model[0].(*ai.SystemMessage)
	if !ok || len(patch.ToolsAdded) != 0 || len(patch.ToolsRemoved) != 0 {
		t.Fatalf("patch = %+v", drafts[0].Model[0])
	}
	if patch.Sections["a"] == nil || *patch.Sections["a"] != "2" || patch.Sections["b"] == nil || *patch.Sections["b"] != "3" {
		t.Fatalf("sections = %+v", patch.Sections)
	}
}

func TestPlanSystemEntriesOrderChange(t *testing.T) {
	message := &ai.SystemMessage{Content: ai.StringOrBlocks{Text: ""}}
	message.SetSection("a", stringPointer("1"))
	message.SetSection("b", stringPointer("2"))
	view := ContextView{Messages: []ai.Message{message}}
	desired := orderedStrings("b", "2", "a", "1")
	drafts := PlanSystemEntries(view, desired, nil, 7)
	if len(drafts) != 2 {
		t.Fatalf("drafts = %+v", drafts)
	}
	remove, _ := drafts[0].Model[0].(*ai.SystemMessage)
	add, _ := drafts[1].Model[0].(*ai.SystemMessage)
	if remove.Sections["a"] != nil || remove.Sections["b"] != nil {
		t.Fatalf("remove = %+v", remove.Sections)
	}
	if add.Sections["a"] == nil || add.Sections["b"] == nil {
		t.Fatalf("add = %+v", add.Sections)
	}
	order := add.SectionOrder()
	if len(order) != 2 || order[0] != "b" || order[1] != "a" {
		t.Fatalf("order = %+v", order)
	}
}

func TestPlanTools(t *testing.T) {
	offered := []ai.Tool{{Name: "a", Description: "a"}, {Name: "b", Description: "b"}}
	desired := []ai.Tool{{Name: "b", Description: "b"}, {Name: "a", Description: "a"}}
	changes := planTools(offered, desired)
	if len(changes.toolsRemoved) != 2 || len(changes.toolsAdded) != 2 {
		t.Fatalf("changes = %+v", changes)
	}
	// A changed declaration is removed and re-added.
	changed := []ai.Tool{{Name: "a", Description: "a2"}}
	changes = planTools(offered, changed)
	if len(changes.toolsRemoved) != 2 || len(changes.toolsAdded) != 1 || changes.toolsAdded[0].Description != "a2" {
		t.Fatalf("changes = %+v", changes)
	}
	// No change yields nothing.
	if changes = planTools(offered, offered); len(changes.toolsRemoved) != 0 || len(changes.toolsAdded) != 0 {
		t.Fatalf("changes = %+v", changes)
	}
}

func TestPlanSections(t *testing.T) {
	shown := orderedStrings("a", "1", "b", "2")
	desired := orderedStrings("a", "1", "b", "3")
	patches := planSections(shown, desired)
	if len(patches) != 1 {
		t.Fatalf("patches = %+v", patches)
	}
	value, _ := patches[0].Get("b")
	if value == nil || *value != "3" {
		t.Fatalf("b = %+v", value)
	}
	// A removal patches null.
	desired = orderedStrings("a", "1")
	patches = planSections(shown, desired)
	if len(patches) != 1 {
		t.Fatalf("patches = %+v", patches)
	}
	removed, _ := patches[0].Get("b")
	if removed != nil {
		t.Fatalf("b = %+v", removed)
	}
	// Identical sections need no patch.
	if patches = planSections(shown, shown); len(patches) != 0 {
		t.Fatalf("patches = %+v", patches)
	}
}
