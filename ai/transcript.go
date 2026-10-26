package ai

import (
	"encoding/json"
	"strings"
)

// normalizeContextInput mirrors upstream's Context for the port of
// utils/transcript.ts. Re-exported here for readability.

// CreateInitialSystemMessage builds the leading system message for a prompt
// and tool set. Returns nil when both are empty, so an empty transcript
// stays empty.
func CreateInitialSystemMessage(systemPrompt *string, tools []Tool) *SystemMessage {
	hasSystemPrompt := systemPrompt != nil && len(*systemPrompt) > 0
	hasTools := len(tools) > 0
	if !hasSystemPrompt && !hasTools {
		return nil
	}
	prompt := ""
	if systemPrompt != nil {
		prompt = *systemPrompt
	}
	m := &SystemMessage{Content: StringOrBlocks{Text: prompt}, Timestamp: 0}
	if hasTools {
		m.ToolsAdded = tools
	}
	return m
}

// NormalizeContext folds Context.SystemPrompt and Context.Tools into a
// leading system message. This is the only entry point that produces a
// TranscriptContext; every provider-facing function expects the result.
func NormalizeContext(context Context) TranscriptContext {
	initial := CreateInitialSystemMessage(context.SystemPrompt, context.Tools)
	var messages []Message
	if initial != nil {
		messages = append([]Message{initial}, context.Messages...)
	} else {
		messages = context.Messages
	}
	return TranscriptContext{Messages: messages}
}

// RoleOf returns a message's role (upstream's `role` data field).
func RoleOf(m Message) Role { return m.messageRole() }

// systemMessageOf narrows a message to *SystemMessage by role.
func systemMessageOf(m Message) *SystemMessage {
	if s, ok := m.(*SystemMessage); ok {
		return s
	}
	return nil
}

// GetInitialSystemMessage returns the leading system message, if the
// transcript starts with one.
func GetInitialSystemMessage(messages []Message) *SystemMessage {
	if len(messages) == 0 {
		return nil
	}
	return systemMessageOf(messages[0])
}

// WithoutInitialSystemMessage drops the leading system message for APIs that
// carry the prompt outside the message list.
func WithoutInitialSystemMessage(messages []Message) []Message {
	if GetInitialSystemMessage(messages) != nil {
		return messages[1:]
	}
	return messages
}

// GetCurrentTools resolves the tools available after applying every
// transcript delta in order.
func GetCurrentTools(messages []Message) []Tool {
	return currentToolsOfSystemMessages(systemMessagesOf(messages))
}

// currentToolsOfSystemMessages resolves the tool set after applying these
// system messages' deltas in order.
func currentToolsOfSystemMessages(systemMessages []*SystemMessage) []Tool {
	tools := map[string]Tool{}
	var order []string
	for _, s := range systemMessages {
		for _, ref := range s.ToolsRemoved {
			if _, ok := tools[ref.Name]; ok {
				delete(tools, ref.Name)
			}
			for i, name := range order {
				if name == ref.Name {
					order = append(order[:i], order[i+1:]...)
					break
				}
			}
		}
		for _, tool := range s.ToolsAdded {
			if _, ok := tools[tool.Name]; !ok {
				order = append(order, tool.Name)
			}
			tools[tool.Name] = tool
		}
	}
	out := make([]Tool, 0, len(order))
	for _, name := range order {
		if t, ok := tools[name]; ok {
			out = append(out, t)
		}
	}
	return out
}

// GetCurrentSystemMessage replays every system message into one leading
// system message holding the current prompt and tools. Later content is
// appended to the base prompt, sections are patched by name, and tools are
// resolved with GetCurrentTools.
func GetCurrentSystemMessage(messages []Message) *SystemMessage {
	var content []string
	sections := map[string]*string{}
	var sectionOrder []string
	var timestamp *int64
	for _, message := range messages {
		s := systemMessageOf(message)
		if s == nil {
			continue
		}
		if timestamp == nil {
			ts := s.Timestamp
			timestamp = &ts
		}
		if text := ContentText(s.Content, "\n"); len(text) > 0 {
			content = append(content, text)
		}
		for _, name := range s.sectionNames() {
			value, ok := s.Sections[name]
			if !ok {
				continue
			}
			if value == nil {
				// Deleting removes the name's position, so a later re-add moves it
				// to the end like a JS Map.
				delete(sections, name)
				for i, n := range sectionOrder {
					if n == name {
						sectionOrder = append(sectionOrder[:i], sectionOrder[i+1:]...)
						break
					}
				}
				continue
			}
			if _, seen := sections[name]; !seen {
				sectionOrder = append(sectionOrder, name)
			}
			sections[name] = value
		}
	}
	tools := GetCurrentTools(messages)
	if timestamp == nil && len(tools) == 0 {
		return nil
	}
	out := &SystemMessage{
		Content:   StringOrBlocks{Text: strings.Join(content, "\n\n")},
		Timestamp: 0,
	}
	if timestamp != nil {
		out.Timestamp = *timestamp
	}
	for _, name := range sectionOrder {
		if value, ok := sections[name]; ok {
			out.SetSection(name, value)
		}
	}
	if len(tools) > 0 {
		out.ToolsAdded = tools
	}
	return out
}

// GetCurrentSystemPrompt renders the current system prompt text after
// replaying every system message.
func GetCurrentSystemPrompt(messages []Message) string {
	message := GetCurrentSystemMessage(messages)
	if message == nil {
		return ""
	}
	return GetSystemMessageText(message)
}

// CollapseSystemMessages rebuilds the transcript for APIs without
// mid-conversation system messages: the replayed system message leads, and
// every later system message is dropped.
func CollapseSystemMessages(context TranscriptContext) TranscriptContext {
	head := GetCurrentSystemMessage(context.Messages)
	var messages []Message
	for _, m := range context.Messages {
		if systemMessageOf(m) == nil {
			messages = append(messages, m)
		}
	}
	if head != nil {
		messages = append([]Message{head}, messages...)
	}
	return TranscriptContext{Messages: messages}
}

// ResolveTranscript keeps later system messages in place when the model
// accepts them; otherwise collapses them.
func ResolveTranscript(context TranscriptContext, supportsMidConvoSystemMessages bool) TranscriptContext {
	if supportsMidConvoSystemMessages {
		return context
	}
	return CollapseSystemMessages(context)
}

// ToToolDeclaration strips executable and display-only fields from a tool
// before transcript comparison or persistence.
func ToToolDeclaration(tool Tool) Tool {
	// The JSON round-trip drops typebox symbol keys and undefined fields and
	// builds the object with a canonical key order.
	enc, err := json.Marshal(tool)
	if err != nil {
		return tool
	}
	var out Tool
	if err := json.Unmarshal(enc, &out); err != nil {
		return tool
	}
	out.ConstrainedSampling = tool.ConstrainedSampling
	return out
}

// DeclarationsEqual reports whether two tools declare the same interface to
// the model.
func DeclarationsEqual(left, right Tool) bool {
	l, errL := json.Marshal(ToToolDeclaration(left))
	r, errR := json.Marshal(ToToolDeclaration(right))
	if errL != nil || errR != nil {
		return false
	}
	return string(l) == string(r)
}

// ToolStateChanges compares two complete tool states. A changed definition
// is a removal followed by an addition.
type ToolStateChanges struct {
	ToolsAdded   []Tool
	ToolsRemoved []ToolReference
}

// GetToolStateChanges diffs two complete tool states.
func GetToolStateChanges(previous, current []Tool) ToolStateChanges {
	previousTools := map[string]Tool{}
	for _, t := range previous {
		previousTools[t.Name] = t
	}
	currentTools := map[string]Tool{}
	for _, t := range current {
		currentTools[t.Name] = t
	}
	var out ToolStateChanges
	for _, tool := range current {
		prev, ok := previousTools[tool.Name]
		if !ok || !DeclarationsEqual(prev, tool) {
			out.ToolsAdded = append(out.ToolsAdded, ToToolDeclaration(tool))
		}
	}
	for _, tool := range previous {
		cur, ok := currentTools[tool.Name]
		if !ok || !DeclarationsEqual(tool, cur) {
			out.ToolsRemoved = append(out.ToolsRemoved, ToolReference{Name: tool.Name})
		}
	}
	return out
}

// GetDeclaredTools returns every definition referenced by transcript tool
// state, in first-declaration order.
func GetDeclaredTools(messages []Message) []Tool {
	definitions := map[string]Tool{}
	var order []string
	for _, message := range messages {
		s := systemMessageOf(message)
		if s == nil {
			continue
		}
		for _, tool := range s.ToolsAdded {
			if _, ok := definitions[tool.Name]; !ok {
				order = append(order, tool.Name)
			}
			definitions[tool.Name] = tool
		}
	}
	out := make([]Tool, 0, len(order))
	for _, name := range order {
		out = append(out, definitions[name])
	}
	return out
}

// HasToolRedefinitions reports whether a tool name was declared twice with
// different definitions. A transport that can only reference previously
// declared tools by name cannot replay such a history.
//
// Deprecated: no built-in transport needs this anymore; Anthropic expresses
// redefinitions with inline tool_definition blocks (inline-tools-2026-09-15).
// Kept for API compatibility.
func HasToolRedefinitions(messages []Message) bool {
	declared := map[string]Tool{}
	for _, message := range messages {
		s := systemMessageOf(message)
		if s == nil {
			continue
		}
		for _, tool := range s.ToolsAdded {
			if previous, ok := declared[tool.Name]; ok && !DeclarationsEqual(previous, tool) {
				return true
			}
			declared[tool.Name] = tool
		}
	}
	return false
}

// HasNonAdditiveToolChanges reports whether tool history contains a removal
// or same-name redeclaration that an addition-only transport cannot replay.
func HasNonAdditiveToolChanges(messages []Message) bool {
	return findLastNonAdditiveToolChange(systemMessagesOf(messages)) >= 0
}

// systemMessagesOf returns the transcript's system messages in order, which is
// the order transformMessages preserves.
func systemMessagesOf(messages []Message) []*SystemMessage {
	out := make([]*SystemMessage, 0, len(messages))
	for _, message := range messages {
		if s := systemMessageOf(message); s != nil {
			out = append(out, s)
		}
	}
	return out
}

// findLastNonAdditiveToolChange reports the index of the last system message
// with a removal or a same-name redeclaration, or -1.
func findLastNonAdditiveToolChange(systemMessages []*SystemMessage) int {
	// A name declared before counts as a redeclaration even after its removal:
	// Kimi K3 ignores a removed tool that a later tool message declares again.
	declared := map[string]bool{}
	last := -1
	for index, message := range systemMessages {
		if len(message.ToolsRemoved) > 0 {
			last = index
		}
		for _, tool := range message.ToolsAdded {
			if declared[tool.Name] {
				last = index
			}
			declared[tool.Name] = true
		}
	}
	return last
}

// TranscriptToolSupport says how a transport can express tool changes after the
// first request (upstream TranscriptToolSupport).
type TranscriptToolSupport string

const (
	// TranscriptToolSupportNone keeps only the top-level tool list; it always
	// holds the current tools.
	TranscriptToolSupportNone TranscriptToolSupport = "none"
	// TranscriptToolSupportAdditions can add tools at a later message, but not
	// remove or redefine one.
	TranscriptToolSupportAdditions TranscriptToolSupport = "additions"
	// TranscriptToolSupportChanges declares every tool, including the initial
	// ones, in place; a later declaration under the same name replaces the
	// earlier one. Removals are not expressible on the wire, so the transport
	// announces them as text and the removed tools stay declared.
	TranscriptToolSupportChanges TranscriptToolSupport = "changes"
)

// TranscriptTools splits tool declarations between the top-level request field
// and in-place changes.
type TranscriptTools struct {
	// RequestTools are the tools sent in the top-level request field.
	RequestTools []Tool
	// FoldedSystemMessages is the number of leading system messages whose tool
	// changes RequestTools already holds. Every later system message carries its
	// own tool changes in place.
	FoldedSystemMessages int
}

// ResolveTranscriptTools splits tool declarations between the top-level request
// field and in-place changes.
//
// With "changes" the top-level list stays empty and every system message,
// including the leading one, declares its tools in place: an in-place
// declaration can replace an earlier in-place one, but not a top-level one.
// With "additions" a removal or redefinition cannot be expressed in place, so
// the top-level list becomes the tool state after the last such change; later
// additions stay anchored, and the cached prefix is invalidated once per
// removal instead of on every later addition. With "none" the top-level list is
// the current tool set.
func ResolveTranscriptTools(messages []Message, support TranscriptToolSupport) TranscriptTools {
	systemMessages := systemMessagesOf(messages)
	switch support {
	case TranscriptToolSupportNone:
		return TranscriptTools{RequestTools: GetCurrentTools(messages), FoldedSystemMessages: len(systemMessages)}
	case TranscriptToolSupportChanges:
		// Upstream returns an empty array, not a missing value: callers may
		// persist or forward this shape, and null is not [].
		return TranscriptTools{RequestTools: []Tool{}}
	}
	folded := 0
	if GetInitialSystemMessage(messages) != nil {
		folded = 1
	}
	if last := findLastNonAdditiveToolChange(systemMessages) + 1; last > folded {
		folded = last
	}
	return TranscriptTools{RequestTools: currentToolsOfSystemMessages(systemMessages[:folded]), FoldedSystemMessages: folded}
}
