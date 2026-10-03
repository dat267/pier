package durable

import (
	"github.com/dat267/pier/ai"
	"github.com/dat267/pier/chord"
)

// Port of harness/prompt.ts: the system prompt sections and the `pi.system`
// entries that make the replayed sections and tools equal the desired ones.

// ReplaySections is the sections in effect after replaying system messages in
// order: set in place, nil deletes, re-adding appends.
func ReplaySections(messages []ai.Message) *orderedMap[string] {
	shown := newOrderedMap[string]()
	for _, message := range messages {
		system, ok := message.(*ai.SystemMessage)
		if !ok || system.Sections == nil {
			continue
		}
		for _, key := range system.SectionOrder() {
			value := system.Sections[key]
			if value == nil {
				shown.Delete(key)
			} else {
				shown.Set(key, *value)
			}
		}
	}
	return shown
}

// SectionRender is one section's rendered text and whether it is present.
type SectionRender func(input PromptInput, ctx chord.Context) (string, bool, error)

// RenderSections renders the agent's sections in order. A section that reports
// absent is omitted; tagged text is wrapped; a section that fails keeps its
// shown text and is reported, while a failure after the context is cancelled
// propagates.
func RenderSections(sections []PromptSection, input PromptInput, shown *orderedMap[string], report func(error), ctx chord.Context) (*orderedMap[string], error) {
	desired := newOrderedMap[string]()
	for _, section := range sections {
		text, present, err := section.Render(input, ctx)
		if err != nil {
			if ctx.Err() != nil {
				return nil, err
			}
			report(err)
			if kept, ok := shown.Get(section.Key); ok {
				desired.Set(section.Key, kept)
			}
			continue
		}
		if !present {
			continue
		}
		tagged := section.Tag == nil || *section.Tag
		if tagged {
			desired.Set(section.Key, "<"+section.Key+">\n"+text+"\n</"+section.Key+">")
		} else {
			desired.Set(section.Key, text)
		}
	}
	return desired, nil
}

type toolChanges struct {
	toolsRemoved []ai.ToolReference
	toolsAdded   []ai.Tool
}

// PlanSystemEntries plans the `pi.system` entries that make the replayed
// sections and tools of view equal desired and tools in values and order.
func PlanSystemEntries(view ContextView, desired *orderedMap[string], tools []ai.Tool, timestamp int64) []EntryDraft {
	head := view.Head
	if head != nil && !hasSystemEntryAfter(view.Entries, head.ID) {
		edits := []ContextEdit{}
		for _, entry := range view.Entries {
			if SystemEntry.Matches(&entry) {
				edits = append(edits, ContextEdit{Target: entry.ID, Action: EditOmit})
			}
		}
		baseline := systemEntry(sectionsOf(desired), &toolChanges{toolsAdded: declarations(tools)}, timestamp)
		if len(edits) > 0 {
			baseline.Edits = edits
		}
		return []EntryDraft{baseline}
	}
	sections := planSections(ReplaySections(view.Messages), desired)
	changes := planTools(ai.GetCurrentTools(view.Messages), tools)
	if len(changes.toolsRemoved) == 0 && len(changes.toolsAdded) == 0 {
		out := make([]EntryDraft, 0, len(sections))
		for _, patch := range sections {
			out = append(out, systemEntry(patch, nil, timestamp))
		}
		return out
	}
	if len(sections) == 0 {
		return []EntryDraft{systemEntry(nil, &changes, timestamp)}
	}
	out := make([]EntryDraft, 0, len(sections))
	for index, patch := range sections {
		if index == len(sections)-1 {
			out = append(out, systemEntry(patch, &changes, timestamp))
		} else {
			out = append(out, systemEntry(patch, nil, timestamp))
		}
	}
	return out
}

// planTools is the tool changes from offered to desired. A changed declaration
// is removed and re-added; when replaying would not yield the desired order,
// every offered tool is removed and every desired one re-added.
func planTools(offered, desired []ai.Tool) toolChanges {
	wanted := make(map[string]ai.Tool, len(desired))
	for _, tool := range desired {
		wanted[tool.Name] = tool
	}
	kept := []ai.Tool{}
	keptNames := map[string]bool{}
	for _, tool := range offered {
		next, present := wanted[tool.Name]
		if present && ai.DeclarationsEqual(tool, next) {
			kept = append(kept, tool)
			keptNames[tool.Name] = true
		}
	}
	added := []ai.Tool{}
	for _, tool := range desired {
		if !keptNames[tool.Name] {
			added = append(added, tool)
		}
	}
	replayed := append(append([]ai.Tool{}, kept...), added...)
	mismatch := len(replayed) != len(desired)
	if !mismatch {
		for index := range replayed {
			if replayed[index].Name != desired[index].Name {
				mismatch = true
				break
			}
		}
	}
	if mismatch {
		removed := []ai.ToolReference{}
		for _, tool := range offered {
			removed = append(removed, ai.ToolReference{Name: tool.Name})
		}
		return toolChanges{toolsRemoved: removed, toolsAdded: declarations(desired)}
	}
	removed := []ai.ToolReference{}
	for _, tool := range offered {
		if !keptNames[tool.Name] {
			removed = append(removed, ai.ToolReference{Name: tool.Name})
		}
	}
	return toolChanges{toolsRemoved: removed, toolsAdded: declarations(added)}
}

// planSections is none, the minimal patch, or a remove-all/re-add-all pair
// when the order would differ.
func planSections(shown, desired *orderedMap[string]) []*orderedMap[*string] {
	patchedOrder := []string{}
	for _, key := range shown.Keys() {
		if _, present := desired.Get(key); present {
			patchedOrder = append(patchedOrder, key)
		}
	}
	for _, key := range desired.Keys() {
		if _, present := shown.Get(key); !present {
			patchedOrder = append(patchedOrder, key)
		}
	}
	desiredOrder := desired.Keys()
	mismatch := len(patchedOrder) != len(desiredOrder)
	if !mismatch {
		for index := range patchedOrder {
			if patchedOrder[index] != desiredOrder[index] {
				mismatch = true
				break
			}
		}
	}
	if mismatch {
		removeAll := newOrderedMap[*string]()
		for _, key := range shown.Keys() {
			removeAll.Set(key, nil)
		}
		addAll := newOrderedMap[*string]()
		for _, key := range desired.Keys() {
			value, _ := desired.Get(key)
			addAll.Set(key, &value)
		}
		return []*orderedMap[*string]{removeAll, addAll}
	}
	patch := newOrderedMap[*string]()
	for _, key := range shown.Keys() {
		current, _ := shown.Get(key)
		next, present := desired.Get(key)
		switch {
		case !present:
			patch.Set(key, nil)
		case next != current:
			value := next
			patch.Set(key, &value)
		}
	}
	for _, key := range desired.Keys() {
		if _, present := shown.Get(key); present {
			continue
		}
		value, _ := desired.Get(key)
		patch.Set(key, &value)
	}
	if len(patch.Keys()) == 0 {
		return nil
	}
	return []*orderedMap[*string]{patch}
}

// systemEntry builds the `pi.system` entry draft.
func systemEntry(sections *orderedMap[*string], tools *toolChanges, timestamp int64) EntryDraft {
	message := &ai.SystemMessage{Content: ai.StringOrBlocks{Text: ""}, Timestamp: timestamp}
	if sections != nil {
		for _, key := range sections.Keys() {
			value, _ := sections.Get(key)
			message.SetSection(key, value)
		}
	}
	if tools != nil && len(tools.toolsRemoved) > 0 {
		message.ToolsRemoved = tools.toolsRemoved
	}
	if tools != nil && len(tools.toolsAdded) > 0 {
		message.ToolsAdded = tools.toolsAdded
	}
	return EntryDraft{Kind: SystemEntry.Kind, Model: []ai.Message{message}}
}

func sectionsOf(desired *orderedMap[string]) *orderedMap[*string] {
	sections := newOrderedMap[*string]()
	for _, key := range desired.Keys() {
		value, _ := desired.Get(key)
		sections.Set(key, &value)
	}
	return sections
}

func declarations(tools []ai.Tool) []ai.Tool {
	out := make([]ai.Tool, 0, len(tools))
	for _, tool := range tools {
		out = append(out, ai.ToToolDeclaration(tool))
	}
	return out
}

func hasSystemEntryAfter(entries []EntryRecord, id Id) bool {
	for index := range entries {
		entry := entries[index]
		if SystemEntry.Matches(&entry) && entry.ID > id {
			return true
		}
	}
	return false
}
