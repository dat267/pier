package coding

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/dat267/pier/ai"
)

// Port of core/compaction/compaction.ts and compaction/utils.ts: context
// compaction for long sessions (pure functions; the session manager does I/O).
// The user's compaction extension is not built in; extension
// mechanics stay out of scope (D41).

// CompactionSettings tune automatic compaction.
type CompactionSettings struct {
	Enabled          bool
	ReserveTokens    int64
	KeepRecentTokens int64
}

// DefaultCompactionSettings are the upstream defaults.
var DefaultCompactionSettings = CompactionSettings{
	Enabled: true, ReserveTokens: 16384, KeepRecentTokens: 20000,
}

// CompactionDetails is the file-tracking payload in CompactionEntry.details.
type CompactionDetails struct {
	ReadFiles     []string `json:"readFiles"`
	ModifiedFiles []string `json:"modifiedFiles"`
}

// FileOperations tracks file usage across the summarized span. The Order
// slices record first-seen order: Go maps lose insertion order, and the
// capped file lists keep the most recently seen paths (extension summary.ts).
type FileOperations struct {
	Read    map[string]bool
	Written map[string]bool
	Edited  map[string]bool

	ReadOrder    []string
	WrittenOrder []string
	EditedOrder  []string
}

// CreateFileOps builds an empty set.
func CreateFileOps() *FileOperations {
	return &FileOperations{Read: map[string]bool{}, Written: map[string]bool{}, Edited: map[string]bool{}}
}

func (f *FileOperations) markRead(path string) {
	if !f.Read[path] {
		f.Read[path] = true
		f.ReadOrder = append(f.ReadOrder, path)
	}
}

func (f *FileOperations) markWritten(path string) {
	if !f.Written[path] {
		f.Written[path] = true
		f.WrittenOrder = append(f.WrittenOrder, path)
	}
}

func (f *FileOperations) markEdited(path string) {
	if !f.Edited[path] {
		f.Edited[path] = true
		f.EditedOrder = append(f.EditedOrder, path)
	}
}

// ExtractFileOpsFromMessage extracts file paths from a tool call
// (read/write/edit) in an assistant message, or from the nested calls recorded
// on a tool result (upstream extractFileOpsFromMessage).
func ExtractFileOpsFromMessage(message ai.Message, fileOps *FileOperations) {
	switch m := message.(type) {
	case *ai.ToolResultMessage:
		// Calls made from codemode scripts are recorded on the script's result.
		if m.NestedCalls == nil {
			return
		}
		for _, call := range m.NestedCalls.Calls {
			addFileOp(call.Name, call.Arguments, fileOps)
		}
		return
	case *ai.AssistantMessage:
		for _, block := range m.Content {
			call, ok := block.(ai.ToolCall)
			if !ok {
				continue
			}
			addFileOp(call.Name, call.Arguments, fileOps)
		}
	}
}

// addFileOp records one read/write/edit call's path.
func addFileOp(toolName string, arguments json.RawMessage, fileOps *FileOperations) {
	var args struct {
		Path string `json:"path"`
	}
	if json.Unmarshal(arguments, &args) != nil || args.Path == "" {
		return
	}
	switch toolName {
	case "read":
		fileOps.markRead(args.Path)
	case "write":
		fileOps.markWritten(args.Path)
	case "edit":
		fileOps.markEdited(args.Path)
	}
}

// ComputeFileLists finalizes the file lists: read-only files (read but never
// modified) and modified files, sorted. Always non-nil (upstream emits []).
func ComputeFileLists(fileOps *FileOperations) (readFiles, modifiedFiles []string) {
	readFiles = []string{}
	modifiedFiles = []string{}
	modified := map[string]bool{}
	for f := range fileOps.Edited {
		modified[f] = true
	}
	for f := range fileOps.Written {
		modified[f] = true
	}
	for f := range fileOps.Read {
		if !modified[f] {
			readFiles = append(readFiles, f)
		}
	}
	sortStrings(readFiles)
	for f := range modified {
		modifiedFiles = append(modifiedFiles, f)
	}
	sortStrings(modifiedFiles)
	return readFiles, modifiedFiles
}

// FormatFileOperations renders the XML file lists for the summary.
func FormatFileOperations(readFiles, modifiedFiles []string) string {
	var sections []string
	if len(readFiles) > 0 {
		sections = append(sections, "<read-files>\n"+strings.Join(readFiles, "\n")+"\n</read-files>")
	}
	if len(modifiedFiles) > 0 {
		sections = append(sections, "<modified-files>\n"+strings.Join(modifiedFiles, "\n")+"\n</modified-files>")
	}
	if len(sections) == 0 {
		return ""
	}
	return "\n\n" + strings.Join(sections, "\n\n")
}

// ============================================================================
// Token calculation (compaction-specific: iterates AgentMessages including
// custom roles).
// ============================================================================

const estimatedImageChars = 4800

// CalculateCompactionContextTokens of a usage block (native totalTokens when
// available).
func CalculateCompactionContextTokens(usage ai.Usage) int {
	if usage.TotalTokens != 0 {
		return int(usage.TotalTokens)
	}
	return int(usage.Input + usage.Output + usage.CacheRead + usage.CacheWrite)
}

// getAssistantUsage skips aborted, error, and all-zero usage messages.
func getAssistantUsage(msg ai.Message) *ai.Usage {
	assistant, ok := msg.(*ai.AssistantMessage)
	if !ok {
		return nil
	}
	if assistant.StopReason != ai.StopAborted && assistant.StopReason != ai.StopError &&
		CalculateCompactionContextTokens(assistant.Usage) > 0 {
		return &assistant.Usage
	}
	return nil
}

// customMessageChars counts a custom message's text for estimation: the
// content field for a custom message, command+output for bashExecution, and the
// summary for the summary roles (upstream estimateTokens).
func customMessageChars(message *ai.CustomMessage) int {
	switch message.Role {
	case RoleBashExecution:
		var fields bashExecutionFields
		if json.Unmarshal(message.Content, &fields) == nil {
			return JSLength(fields.Command) + JSLength(fields.Output)
		}
	case RoleBranchSummary, RoleCompactionSummary:
		var fields struct {
			Summary string `json:"summary"`
		}
		if json.Unmarshal(message.Content, &fields) == nil {
			return JSLength(fields.Summary)
		}
	default:
		var fields customMessageFields
		if json.Unmarshal(message.Content, &fields) == nil {
			var content ai.StringOrBlocks
			if json.Unmarshal(fields.Content, &content) == nil {
				return estimateTextAndImageContentChars(content)
			}
		}
	}
	return 0
}

// EstimateAgentMessageTokens estimates tokens with the chars/4 heuristic
// (conservative; port of estimateTokens).
func EstimateAgentMessageTokens(message ai.Message) int {
	chars := 0
	switch m := message.(type) {
	case *ai.UserMessage:
		if m.Content.Blocks == nil {
			chars = JSLength(m.Content.Text)
		} else {
			for _, block := range m.Content.Blocks {
				switch b := block.(type) {
				case ai.TextContent:
					chars += JSLength(b.Text)
				case ai.ImageContent:
					chars += estimatedImageChars
				}
			}
		}
		return ceilDiv(chars, 4)
	case *ai.AssistantMessage:
		for _, block := range m.Content {
			switch b := block.(type) {
			case ai.TextContent:
				chars += JSLength(b.Text)
			case ai.ThinkingContent:
				chars += JSLength(b.Thinking)
			case ai.ToolCall:
				chars += JSLength(b.Name) + JSLength(string(mustMarshalJSON(json.RawMessage(b.Arguments))))
			}
		}
		return ceilDiv(chars, 4)
	case *ai.ToolResultMessage:
		for _, block := range m.Content {
			switch b := block.(type) {
			case ai.TextContent:
				chars += JSLength(b.Text)
			case ai.ImageContent:
				chars += estimatedImageChars
			}
		}
		return ceilDiv(chars, 4)
	case *ai.CustomMessage:
		return ceilDiv(customMessageChars(m), 4)
	case *ai.SystemMessage:
		// Upstream sums the content chars, each section's length, and
		// JSON.stringify(toolsAdded).length; it does not use the rendered prompt
		// text (which would count the "\n\n" separators and omit toolsAdded).
		chars := estimateTextAndImageContentChars(m.Content)
		for _, section := range m.Sections {
			if section != nil {
				chars += JSLength(*section)
			}
		}
		if m.ToolsAdded != nil {
			chars += JSLength(string(mustMarshalJSON(m.ToolsAdded)))
		}
		return ceilDiv(chars, 4)
	default:
		return 0
	}
}

// estimateTextAndImageContentChars matches upstream's helper: a string content
// is its length, block content sums text and a fixed image estimate.
func estimateTextAndImageContentChars(content ai.StringOrBlocks) int {
	if content.Blocks == nil {
		return JSLength(content.Text)
	}
	chars := 0
	for _, block := range content.Blocks {
		switch b := block.(type) {
		case ai.TextContent:
			chars += JSLength(b.Text)
		case ai.ImageContent:
			chars += estimatedImageChars
		}
	}
	return chars
}

// CompactionContextUsageEstimate is the estimate outcome.
type CompactionContextUsageEstimate struct {
	Tokens         int
	UsageTokens    int
	TrailingTokens int
	LastUsageIndex int
}

// EstimateContextTokens estimates from messages, preferring the last valid
// assistant usage and estimating only the trailing messages.
func EstimateContextTokens(messages []ai.Message) CompactionContextUsageEstimate {
	usageInfo := -1
	var usage ai.Usage
	for i := len(messages) - 1; i >= 0; i-- {
		if u := getAssistantUsage(messages[i]); u != nil {
			usage = *u
			usageInfo = i
			break
		}
	}
	if usageInfo == -1 {
		estimated := 0
		for _, message := range messages {
			estimated += EstimateAgentMessageTokens(message)
		}
		return CompactionContextUsageEstimate{Tokens: estimated, UsageTokens: 0, TrailingTokens: estimated, LastUsageIndex: -1}
	}
	usageTokens := CalculateCompactionContextTokens(usage)
	trailing := 0
	for i := usageInfo + 1; i < len(messages); i++ {
		trailing += EstimateAgentMessageTokens(messages[i])
	}
	return CompactionContextUsageEstimate{
		Tokens: usageTokens + trailing, UsageTokens: usageTokens,
		TrailingTokens: trailing, LastUsageIndex: usageInfo,
	}
}

// EstimateProjectedContextTokens estimates the tokens a projection carries
// (port of estimateProjectedContextTokens). Provider usage is trusted only when
// the message it came from still sits after the latest context edit or
// compaction; otherwise the usage describes an older context and the estimate
// falls back to message sizes.
func EstimateProjectedContextTokens(window projectedWindow) CompactionContextUsageEstimate {
	estimate := EstimateContextTokens(window.Messages)
	if estimate.LastUsageIndex >= 0 {
		// Find the entry that contributed the usage-bearing message.
		projectedIndex := 0
		usageEntryID := ""
		for _, entry := range window.Entries {
			nextIndex := projectedIndex + len(entry.Messages)
			if usageEntryID == "" && estimate.LastUsageIndex < nextIndex {
				usageEntryID = entry.Source.ID
			}
			projectedIndex = nextIndex
		}
		// Compare provenance in branch order (upstream maps the usage entry
		// into branchEntries): the window replays its newest compaction first,
		// so replay positions do not preserve chronology. A context edit or
		// compaction changes what the messages after it mean; the newest
		// invalidating entry decides.
		usageEntryIndex := -1
		latestInvalidatingIndex := -1
		for index, entry := range window.Branch {
			if usageEntryID != "" && entry.ID == usageEntryID {
				usageEntryIndex = index
			}
			if entry.Type == "context_edit" || entry.Type == "compaction" {
				latestInvalidatingIndex = index
			}
		}
		if usageEntryIndex > latestInvalidatingIndex {
			return estimate
		}
	}

	// Without trustworthy usage, sum the current system prompt and every
	// non-system projected message.
	tokens := 0
	if system := ai.GetCurrentSystemMessage(window.Messages); system != nil {
		tokens += EstimateAgentMessageTokens(system)
	}
	for _, message := range window.Messages {
		if ai.RoleOf(message) != ai.RoleSystem {
			tokens += EstimateAgentMessageTokens(message)
		}
	}
	return CompactionContextUsageEstimate{Tokens: tokens, UsageTokens: 0, TrailingTokens: tokens, LastUsageIndex: -1}
}

// ShouldCompact reports whether compaction should trigger.
func ShouldCompact(contextTokens int64, contextWindow int64, settings CompactionSettings) bool {
	if !settings.Enabled {
		return false
	}
	return contextTokens > contextWindow-settings.ReserveTokens
}

// ============================================================================
// Cut points
// ============================================================================

func isCutPointMessage(message ai.Message) bool {
	switch ai.RoleOf(message) {
	case ai.RoleUser, ai.RoleAssistant, RoleBashExecution, RoleCustom, RoleBranchSummary, RoleCompactionSummary:
		return true
	}
	return false // toolResult
}

func isTurnStartMessage(message ai.Message) bool {
	switch ai.RoleOf(message) {
	case ai.RoleUser, RoleBashExecution, RoleCustom, RoleBranchSummary, RoleCompactionSummary:
		return true
	}
	return false // assistant, toolResult
}

func isTurnStartEntry(entry *SessionEntry) bool {
	if entry.Type == "compaction" {
		return false
	}
	for _, message := range SessionEntryToContextMessages(entry) {
		if isTurnStartMessage(message) {
			return true
		}
	}
	return false
}

// findValidCutPoints finds indices of context-visible user-like or assistant
// messages; never cut at tool results (they must follow their tool call).
func findValidCutPoints(entries []SessionEntry, startIndex, endIndex int) []int {
	var cutPoints []int
	for i := startIndex; i < endIndex; i++ {
		entry := entries[i]
		if entry.Type == "compaction" {
			continue
		}
		for _, message := range SessionEntryToContextMessages(&entry) {
			if isCutPointMessage(message) {
				cutPoints = append(cutPoints, i)
				break
			}
		}
	}
	return cutPoints
}

// FindTurnStartIndex finds the user-role message starting the turn containing
// entryIndex; -1 when none.
func FindTurnStartIndex(entries []SessionEntry, entryIndex, startIndex int) int {
	for i := entryIndex; i >= startIndex; i-- {
		if isTurnStartEntry(&entries[i]) {
			return i
		}
	}
	return -1
}

// CutPointResult is the findCutPoint outcome.
type CutPointResult struct {
	// FirstKeptEntryIndex is the first entry to keep.
	FirstKeptEntryIndex int
	// TurnStartIndex is the user message starting the split turn, or -1.
	TurnStartIndex int
	// IsSplitTurn reports a mid-turn cut.
	IsSplitTurn bool
}

// isProjectedTurnStart reports whether a projected entry starts a turn.
func isProjectedTurnStart(entry ProjectedEntry) bool {
	if entry.Source.Type == "compaction" {
		return false
	}
	for _, message := range entry.Messages {
		if isTurnStartMessage(message) {
			return true
		}
	}
	return false
}

// findProjectedTurnStartIndex finds the projected turn start containing
// entryIndex; -1 when none.
func findProjectedTurnStartIndex(entries []ProjectedEntry, entryIndex, startIndex int) int {
	for i := entryIndex; i >= startIndex; i-- {
		if isProjectedTurnStart(entries[i]) {
			return i
		}
	}
	return -1
}

// projectedEntryIntrinsicallyVisible reports whether the entry's raw
// contribution is context-visible, ignoring context edits (upstream
// isIntrinsicallyVisible).
func projectedEntryIntrinsicallyVisible(entry ProjectedEntry) bool {
	return entry.Source.Type != "context_edit" && len(SessionEntryToContextMessages(entry.Source)) > 0
}

// projectedEntryOmitted reports a context-visible entry that a context edit
// removed from the projection.
func projectedEntryOmitted(entry ProjectedEntry) bool {
	return projectedEntryIntrinsicallyVisible(entry) && len(entry.Messages) == 0
}

// contextEditIsOmission reports a null replacement (an omission).
func contextEditIsOmission(replacement json.RawMessage) bool {
	return bytes.Equal(bytes.TrimSpace(replacement), []byte("null"))
}

// findProjectedCutPoint is findCutPoint over the projection (upstream
// findProjectedCutPoint). Omitted entries carry no messages, so they cost no
// budget, and a closed recovery-omission suffix at the end advances the cut.
func findProjectedCutPoint(entries []ProjectedEntry, startIndex, endIndex, keepRecentTokens int) CutPointResult {
	var cutPoints []int
	for i := startIndex; i < endIndex; i++ {
		if entries[i].Source.Type == "compaction" {
			continue
		}
		for _, message := range entries[i].Messages {
			if isCutPointMessage(message) {
				cutPoints = append(cutPoints, i)
				break
			}
		}
	}
	if len(cutPoints) == 0 {
		return CutPointResult{FirstKeptEntryIndex: startIndex, TurnStartIndex: -1}
	}

	accumulatedTokens := 0
	exceededBudget := false
	cutIndex := cutPoints[0]
	for i := endIndex - 1; i >= startIndex; i-- {
		messageTokens := 0
		for _, message := range entries[i].Messages {
			messageTokens += EstimateAgentMessageTokens(message)
		}
		if messageTokens == 0 {
			continue
		}
		accumulatedTokens += messageTokens
		if accumulatedTokens >= keepRecentTokens {
			exceededBudget = true
			cutIndex = cutPoints[len(cutPoints)-1]
			for _, candidate := range cutPoints {
				if candidate >= i {
					cutIndex = candidate
					break
				}
			}
			break
		}
	}

	// A recovery attempt and its omission edits are context-invisible after the
	// last visible input. Advance only for a closed suffix containing an omitted
	// assistant attempt; arbitrary metadata must not move the cut past unsent
	// input.
	if cutIndex+1 <= endIndex {
		suffix := entries[cutIndex+1 : endIndex]
		omittedSuffixIDs := map[string]bool{}
		for _, entry := range suffix {
			if projectedEntryOmitted(entry) {
				omittedSuffixIDs[entry.Source.ID] = true
			}
		}
		hasExternalReplacement := false
		hasOmittedAssistant := false
		allClosed := true
		for _, entry := range suffix {
			if entry.Source.Type == "context_edit" && !contextEditIsOmission(entry.Source.Replacement) &&
				!omittedSuffixIDs[entry.Source.TargetID] {
				hasExternalReplacement = true
			}
			if projectedEntryHasOmittedAssistant(entry) {
				hasOmittedAssistant = true
			}
			if entry.Source.Type == "compaction" ||
				(projectedEntryIntrinsicallyVisible(entry) && !projectedEntryOmitted(entry)) {
				allClosed = false
			}
		}
		if exceededBudget && !hasExternalReplacement && hasOmittedAssistant && allClosed {
			cutIndex++
		}
	}

	// Back up over adjacent metadata entries that do not affect context.
	for cutIndex > startIndex {
		previous := entries[cutIndex-1]
		if previous.Source.Type == "compaction" || len(previous.Messages) > 0 {
			break
		}
		cutIndex--
	}

	startsTurn := isProjectedTurnStart(entries[cutIndex])
	turnStartIndex := -1
	if !startsTurn {
		turnStartIndex = findProjectedTurnStartIndex(entries, cutIndex, startIndex)
	}
	return CutPointResult{
		FirstKeptEntryIndex: cutIndex, TurnStartIndex: turnStartIndex,
		IsSplitTurn: !startsTurn && turnStartIndex != -1,
	}
}

// projectedEntryHasOmittedAssistant reports an omitted assistant message entry
// (upstream's sourceEntry.message.role === "assistant" && isOmitted(entry)).
func projectedEntryHasOmittedAssistant(entry ProjectedEntry) bool {
	if !projectedEntryOmitted(entry) {
		return false
	}
	for _, message := range SessionEntryToContextMessages(entry.Source) {
		if ai.RoleOf(message) == ai.RoleAssistant {
			return true
		}
	}
	return false
}

// FindCutPoint finds the cut point keeping approximately keepRecentTokens
// (walk backwards from newest accumulating estimated sizes).
func FindCutPoint(entries []SessionEntry, startIndex, endIndex, keepRecentTokens int) CutPointResult {
	cutPoints := findValidCutPoints(entries, startIndex, endIndex)
	if len(cutPoints) == 0 {
		return CutPointResult{FirstKeptEntryIndex: startIndex, TurnStartIndex: -1}
	}

	accumulatedTokens := 0
	cutIndex := cutPoints[0]

	for i := endIndex - 1; i >= startIndex; i-- {
		messageTokens := 0
		for _, message := range SessionEntryToContextMessages(&entries[i]) {
			messageTokens += EstimateAgentMessageTokens(message)
		}
		if messageTokens == 0 {
			continue
		}
		accumulatedTokens += messageTokens
		if accumulatedTokens >= keepRecentTokens {
			// Prefer the closest valid cut point at or after this entry; if
			// trailing tool results exceed the budget alone, keep their
			// preceding assistant tool call.
			cutIndex = cutPoints[len(cutPoints)-1]
			for _, candidate := range cutPoints {
				if candidate >= i {
					cutIndex = candidate
					break
				}
			}
			break
		}
	}

	// Back up over adjacent metadata entries that do not affect context.
	for cutIndex > startIndex {
		prevEntry := entries[cutIndex-1]
		if prevEntry.Type == "compaction" || len(SessionEntryToContextMessages(&prevEntry)) > 0 {
			break
		}
		cutIndex--
	}

	cutEntry := entries[cutIndex]
	startsTurn := isTurnStartEntry(&cutEntry)
	turnStartIndex := -1
	if !startsTurn {
		turnStartIndex = FindTurnStartIndex(entries, cutIndex, startIndex)
	}
	return CutPointResult{
		FirstKeptEntryIndex: cutIndex, TurnStartIndex: turnStartIndex,
		IsSplitTurn: !startsTurn && turnStartIndex != -1,
	}
}

// ============================================================================
// Message serialization for summarization
// ============================================================================

const toolResultMaxChars = 2000

func truncateForSummary(text string, maxChars int) string {
	if JSLength(text) <= maxChars {
		return text
	}
	truncatedChars := JSLength(text) - maxChars
	return JSSlice(text, 0, maxChars) + fmt.Sprintf("\n\n[... %d more characters truncated]", truncatedChars)
}

// marshalOrderedJSON re-serializes raw JSON preserving object key order like
// JSON.stringify; Go's encoding/json sorts map keys.
func marshalOrderedJSON(raw json.RawMessage) string {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return string(raw)
	}
	switch trimmed[0] {
	case '{':
		keys, values, err := readJSONObjectOrdered(trimmed)
		if err != nil {
			return string(raw)
		}
		parts := make([]string, 0, len(keys))
		for _, key := range keys {
			parts = append(parts, string(mustMarshalJSON(key))+":"+marshalOrderedJSON(values[key]))
		}
		return "{" + strings.Join(parts, ",") + "}"
	case '[':
		var elements []json.RawMessage
		if json.Unmarshal(trimmed, &elements) != nil {
			return string(raw)
		}
		parts := make([]string, 0, len(elements))
		for _, element := range elements {
			parts = append(parts, marshalOrderedJSON(element))
		}
		return "[" + strings.Join(parts, ",") + "]"
	default:
		var value any
		if json.Unmarshal(trimmed, &value) == nil {
			return string(mustMarshalJSON(value))
		}
		return string(raw)
	}
}

// SerializeConversation renders LLM messages as text so the model summarizes
// rather than continues (port of serializeConversation).
func SerializeConversation(messages []ai.Message) string {
	var parts []string
	for _, msg := range messages {
		switch m := msg.(type) {
		case *ai.UserMessage:
			content := contentTextJoinedNoSep(m.Content)
			if content != "" {
				parts = append(parts, "[User]: "+content)
			}
		case *ai.AssistantMessage:
			var toolCalls []string
			hasText := false
			for _, block := range m.Content {
				switch b := block.(type) {
				case ai.ToolCall:
					// Upstream serializes Object.entries(args), preserving the
					// model's key order at every level.
					keys, values, err := readJSONObjectOrdered(b.Arguments)
					var argPairs []string
					if err == nil {
						for _, k := range keys {
							argPairs = append(argPairs, fmt.Sprintf("%s=%s", k, marshalOrderedJSON(values[k])))
						}
					}
					toolCalls = append(toolCalls, fmt.Sprintf("%s(%s)", b.Name, strings.Join(argPairs, ", ")))
				case ai.TextContent:
					hasText = true
				}
			}
			if hasText {
				parts = append(parts, "[Assistant]: "+contentTextJoinedNewline(ai.StringOrBlocks{Blocks: m.Content}))
			}
			if len(toolCalls) > 0 {
				parts = append(parts, "[Assistant tool calls]: "+strings.Join(toolCalls, "; "))
			}
		case *ai.ToolResultMessage:
			content := contentTextJoinedNoSep(StringOrBlocksOf(m.Content))
			if content != "" {
				parts = append(parts, "[Tool result]: "+truncateForSummary(content, toolResultMaxChars))
			}
		}
	}
	return strings.Join(parts, "\n\n")
}

// StringOrBlocksOf adapts user content for text joining.
func StringOrBlocksOf(content ai.UserContentList) ai.StringOrBlocks {
	blocks := make(ai.ContentList, 0, len(content))
	for _, c := range content {
		blocks = append(blocks, c)
	}
	return ai.StringOrBlocks{Blocks: blocks}
}

func contentTextJoinedNoSep(content ai.StringOrBlocks) string {
	return contentTextWithSeparator(content, "")
}

// contentTextJoinedNewline is upstream contentText's default separator.
func contentTextJoinedNewline(content ai.StringOrBlocks) string {
	return contentTextWithSeparator(content, "\n")
}

func contentTextWithSeparator(content ai.StringOrBlocks, separator string) string {
	if content.Blocks == nil {
		return content.Text
	}
	var texts []string
	for _, block := range content.Blocks {
		if tc, ok := block.(ai.TextContent); ok {
			texts = append(texts, tc.Text)
		}
	}
	return strings.Join(texts, separator)
}

// ============================================================================
// Summarization
// ============================================================================

const SummarizationSystemPrompt = `You are a context summarization assistant. Your task is to read a conversation between a user and an AI assistant, then produce a structured summary following the exact format specified.

Do NOT continue the conversation. Do NOT respond to any questions in the conversation. ONLY output the structured summary.`

const summarizationPrompt = `The messages above are a conversation to summarize. Create a structured context checkpoint summary that another LLM will use to continue the work.

Use this EXACT format:

## Goal
[What is the user trying to accomplish? Can be multiple items if the session covers different tasks.]

## Constraints & Preferences
- [Any constraints, preferences, or requirements mentioned by user]
- [Or "(none)" if none were mentioned]

## Progress
### Done
- [x] [Completed tasks/changes]

### In Progress
- [ ] [Current work]

### Blocked
- [Issues preventing progress, if any]

## Key Decisions
- **[Decision]**: [Brief rationale]

## Next Steps
1. [Ordered list of what should happen next]

## Critical Context
- [Any data, examples, or references needed to continue]
- [Or "(none)" if not applicable]

Keep each section concise. Preserve exact file paths, function names, and error messages.`

const updateSummarizationPrompt = `The messages above are NEW conversation messages to incorporate into the existing summary provided in <previous-summary> tags.

Update the existing structured summary with new information. RULES:
- PRESERVE all existing information from the previous summary
- ADD new progress, decisions, and context from the new messages
- UPDATE the Progress section: move items from "In Progress" to "Done" when completed
- UPDATE "Next Steps" based on what was accomplished
- PRESERVE exact file paths, function names, and error messages
- If something is no longer relevant, you may remove it

Use this EXACT format:

## Goal
[Preserve existing goals, add new ones if the task expanded]

## Constraints & Preferences
- [Preserve existing, add new ones discovered]

## Progress
### Done
- [x] [Include previously done items AND newly completed items]

### In Progress
- [ ] [Current work - update based on progress]

### Blocked
- [Current blockers - remove if resolved]

## Key Decisions
- **[Decision]**: [Brief rationale] (preserve all previous, add new)

## Next Steps
1. [Update based on current state]

## Critical Context
- [Preserve important context, add new if needed]

Keep each section concise. Preserve exact file paths, function names, and error messages.`

const turnPrefixSummarizationPrompt = `The messages above are earlier context from an ongoing conversation. Later messages are stored separately and do not need to be reconstructed.

Create a concise checkpoint of the user's request and the progress shown above. This checkpoint will be placed before the later messages so the conversation can continue with the necessary context.

## Original Request
[What did the user ask for?]

## Progress So Far
- [Key decisions and work completed in these messages]

## Context Needed to Continue
- [Information from these messages needed to understand the later work]

Only summarize information explicitly present above. Do not infer or recreate later messages.`

// GetSummarizationFailure reports why a summarization response cannot safely
// be persisted (a length stop holds partial text).
func GetSummarizationFailure(response *ai.AssistantMessage, label string) string {
	if response.StopReason == ai.StopError {
		message := "Unknown error"
		if response.ErrorMessage != nil && *response.ErrorMessage != "" {
			message = *response.ErrorMessage
		}
		return fmt.Sprintf("%s failed: %s", label, message)
	}
	if response.StopReason == ai.StopLength {
		return fmt.Sprintf("%s failed: generation hit the token cap and the summary is incomplete", label)
	}
	return ""
}

// CompactionResult is the compact() outcome.
type CompactionResult struct {
	Summary              string
	FirstKeptEntryID     string
	TokensBefore         int64
	EstimatedTokensAfter int64
	Usage                *ai.Usage
	Details              json.RawMessage
}

// CombineUsage merges two usage blocks (port of combineUsage).
func CombineUsage(first, second ai.Usage) ai.Usage {
	out := ai.Usage{
		Input: first.Input + second.Input, Output: first.Output + second.Output,
		CacheRead:   first.CacheRead + second.CacheRead,
		CacheWrite:  first.CacheWrite + second.CacheWrite,
		TotalTokens: first.TotalTokens + second.TotalTokens,
		Cost: ai.UsageCost{
			Input:      first.Cost.Input + second.Cost.Input,
			Output:     first.Cost.Output + second.Cost.Output,
			CacheRead:  first.Cost.CacheRead + second.Cost.CacheRead,
			CacheWrite: first.Cost.CacheWrite + second.Cost.CacheWrite,
			Total:      first.Cost.Total + second.Cost.Total,
		},
	}
	if first.CacheWrite1h != nil || second.CacheWrite1h != nil {
		v := int64(0)
		if first.CacheWrite1h != nil {
			v += *first.CacheWrite1h
		}
		if second.CacheWrite1h != nil {
			v += *second.CacheWrite1h
		}
		out.CacheWrite1h = &v
	}
	if first.Reasoning != nil || second.Reasoning != nil {
		v := int64(0)
		if first.Reasoning != nil {
			v += *first.Reasoning
		}
		if second.Reasoning != nil {
			v += *second.Reasoning
		}
		out.Reasoning = &v
	}
	return out
}

// SummarizationCallbacks hook retry notifications.
type SummarizationCallbacks struct {
	OnRetryScheduled    func(attempt, maxAttempts int, delayMS int64, errorMessage string) error
	OnRetryAttemptStart func() error
	OnRetryFinished     func(success bool, attempt int, finalError string) error
}

// CompleteSummarization wraps one summarization LLM call in the assistant
// retry loop (port of completeSummarization). One-off summaries skip cache
// writes and receive a fresh routing session id when the caller has none.
func CompleteSummarization(
	model *ai.Model,
	context ai.TranscriptContext,
	options *ai.SimpleStreamOptions,
	streamFn StreamFnFn,
	retry *ai.RetryPolicy,
	callbacks *SummarizationCallbacks,
) (*ai.AssistantMessage, error) {
	requestOptions := *options
	requestOptions.CacheRetention = ai.CacheRetentionNone
	if requestOptions.SessionID == "" {
		requestOptions.SessionID = UUIDv7()
	}
	produce := func() (*ai.AssistantMessage, error) {
		if streamFn != nil {
			return streamFn(model, context, &requestOptions).Result(contextOf(&requestOptions))
		}
		return StreamFnDefault(model, context, &requestOptions)
	}
	var cb *ai.RetryCallbacks
	if callbacks != nil {
		cb = &ai.RetryCallbacks{
			OnRetryScheduled:    callbacks.OnRetryScheduled,
			OnRetryAttemptStart: callbacks.OnRetryAttemptStart,
			OnRetryFinished:     callbacks.OnRetryFinished,
		}
	}
	return ai.RetryAssistantCall(produce, retry, contextOf(&requestOptions), cb)
}

// StreamFnFn is the stream function signature for compaction.
type StreamFnFn = func(model *ai.Model, context ai.TranscriptContext, options *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream

// StreamFnDefault is the fallback (nil streamFn → caller-provided default).
var StreamFnDefault = func(model *ai.Model, context ai.TranscriptContext, options *ai.SimpleStreamOptions) (*ai.AssistantMessage, error) {
	return nil, fmt.Errorf("No stream function configured for summarization")
}

func contextOf(options *ai.SimpleStreamOptions) context.Context {
	if options != nil && options.Ctx != nil {
		return options.Ctx
	}
	return context.Background()
}

// CompactionPreparation is the prepareCompaction outcome.
type CompactionPreparation struct {
	FirstKeptEntryID    string
	MessagesToSummarize []ai.Message
	TurnPrefixMessages  []ai.Message
	IsSplitTurn         bool
	TokensBefore        int
	PreviousSummary     string
	FileOps             *FileOperations
	Settings            CompactionSettings
}

// PrepareCompaction pre-computes the compaction cut and spans
// (port of prepareCompaction); nil when there is nothing to compact.
//
// The cut runs over the session projection, not the raw entries: a context edit
// removes or rewrites what the model saw, omitted entries must not cost budget,
// and only projected messages can be summarized.
func PrepareCompaction(pathEntries []SessionEntry, settings CompactionSettings) *CompactionPreparation {
	// Nothing to compact without entries (upstream returns nil).
	if len(pathEntries) == 0 {
		return nil
	}
	if pathEntries[len(pathEntries)-1].Type == "compaction" {
		return nil
	}

	window := projectWindow(materializePointers(pathEntries), nil)
	projectedEntries := window.Entries
	sourceEntries := make([]SessionEntry, len(projectedEntries))
	for i := range projectedEntries {
		sourceEntries[i] = *projectedEntries[i].Source
	}

	// The newest compaction is projected first. Older compaction entries can
	// still occur in its retained raw range, but their projected contribution is
	// empty, so they are skipped here.
	prevCompactionIndex := -1
	for i := range projectedEntries {
		if projectedEntries[i].Source.Type == "compaction" && len(projectedEntries[i].Messages) > 0 {
			prevCompactionIndex = i
			break
		}
	}

	previousSummary := ""
	boundaryStart := 0
	if prevCompactionIndex >= 0 {
		previousSummary = projectedEntries[prevCompactionIndex].Source.Summary
		// The projection has already selected the previous compaction's retained
		// tail, so the boundary follows the compaction entry itself.
		boundaryStart = prevCompactionIndex + 1
	}
	boundaryEnd := len(projectedEntries)
	tokensBefore := EstimateProjectedContextTokens(window).Tokens
	cutPoint := findProjectedCutPoint(projectedEntries, boundaryStart, boundaryEnd, int(settings.KeepRecentTokens))

	if cutPoint.FirstKeptEntryIndex < 0 || cutPoint.FirstKeptEntryIndex >= len(projectedEntries) {
		return nil
	}
	firstKeptEntry := projectedEntries[cutPoint.FirstKeptEntryIndex].Source
	if firstKeptEntry.ID == "" {
		return nil // session needs migration
	}

	historyEnd := cutPoint.FirstKeptEntryIndex
	if cutPoint.IsSplitTurn {
		historyEnd = cutPoint.TurnStartIndex
	}

	var messagesToSummarize []ai.Message
	for i := boundaryStart; i < historyEnd; i++ {
		messagesToSummarize = append(messagesToSummarize, getMessagesFromProjectedEntryForCompaction(projectedEntries[i])...)
	}
	var turnPrefixMessages []ai.Message
	if cutPoint.IsSplitTurn {
		for i := cutPoint.TurnStartIndex; i < cutPoint.FirstKeptEntryIndex; i++ {
			turnPrefixMessages = append(turnPrefixMessages, getMessagesFromProjectedEntryForCompaction(projectedEntries[i])...)
		}
	}
	if len(messagesToSummarize) == 0 && len(turnPrefixMessages) == 0 {
		return nil
	}

	// Extract file operations from edited model-visible messages and the previous
	// compaction.
	fileOps := extractFileOperations(messagesToSummarize, sourceEntries, prevCompactionIndex)
	if cutPoint.IsSplitTurn {
		for _, msg := range turnPrefixMessages {
			ExtractFileOpsFromMessage(msg, fileOps)
		}
	}

	return &CompactionPreparation{
		FirstKeptEntryID: firstKeptEntry.ID, MessagesToSummarize: messagesToSummarize,
		TurnPrefixMessages: turnPrefixMessages, IsSplitTurn: cutPoint.IsSplitTurn,
		TokensBefore: tokensBefore, PreviousSummary: previousSummary,
		FileOps: fileOps, Settings: settings,
	}
}

// extractFileOperations collects file ops from messages and the previous
// pi-generated compaction entry.
func extractFileOperations(messages []ai.Message, entries []SessionEntry, prevCompactionIndex int) *FileOperations {
	fileOps := CreateFileOps()
	if prevCompactionIndex >= 0 && prevCompactionIndex < len(entries) {
		prev := entries[prevCompactionIndex]
		if (prev.FromHook == nil || !*prev.FromHook) && len(prev.Details) > 0 {
			var details CompactionDetails
			if json.Unmarshal(prev.Details, &details) == nil {
				for _, f := range details.ReadFiles {
					fileOps.markRead(f)
				}
				for _, f := range details.ModifiedFiles {
					fileOps.markEdited(f)
				}
			}
		}
	}
	for _, msg := range messages {
		ExtractFileOpsFromMessage(msg, fileOps)
	}
	return fileOps
}

// getMessagesFromProjectedEntryForCompaction returns the model-visible messages
// of a projected entry, dropping compaction entries and system messages (prompt
// state travels with the compaction entry's replay).
func getMessagesFromProjectedEntryForCompaction(entry ProjectedEntry) []ai.Message {
	if entry.Source.Type == "compaction" {
		return nil
	}
	var messages []ai.Message
	for _, message := range entry.Messages {
		if ai.RoleOf(message) != ai.RoleSystem {
			messages = append(messages, message)
		}
	}
	return messages
}

// CompactionOptions carry the LLM access inputs for Compact.
type CompactionOptions struct {
	Model              *ai.Model
	APIKey             string
	Headers            map[string]string
	CustomInstructions string
	Ctx                context.Context
	ThinkingLevel      ai.ThinkingLevel
	StreamFn           StreamFnFn
	Env                map[string]string
	Retry              *ai.RetryPolicy
	Callbacks          *SummarizationCallbacks
	SessionID          string
}

// Compact generates summaries for compaction using prepared data
// (port of compact). The user's compaction extension is not built in:
// the stock summarizer is the only path, like upstream with
// extension mechanics out of scope (D41).
func Compact(preparation *CompactionPreparation, options CompactionOptions) (*CompactionResult, error) {
	var summary string
	var summaryUsage ai.Usage
	hasUsage := false

	if preparation.IsSplitTurn && len(preparation.TurnPrefixMessages) > 0 {
		historyText := preparation.PreviousSummary
		if historyText == "" {
			historyText = "No prior history."
		}
		var historyUsage *ai.Usage
		if len(preparation.MessagesToSummarize) > 0 {
			result, err := generateSummaryWithUsage(preparation.MessagesToSummarize, options, preparation.PreviousSummary)
			if err != nil {
				return nil, err
			}
			historyText = result.text
			historyUsage = result.usage
		}
		turnPrefixResult, err := generateTurnPrefixSummary(preparation.TurnPrefixMessages, options, options.CustomInstructions)
		if err != nil {
			return nil, err
		}
		summary = historyText + "\n\n---\n\n**Turn Context (split turn):**\n\n" + turnPrefixResult.text
		if historyUsage != nil {
			summaryUsage = CombineUsage(*historyUsage, *turnPrefixResult.usage)
		} else {
			summaryUsage = *turnPrefixResult.usage
		}
		hasUsage = true
	} else {
		result, err := generateSummaryWithUsage(preparation.MessagesToSummarize, options, preparation.PreviousSummary)
		if err != nil {
			return nil, err
		}
		summary = result.text
		summaryUsage = *result.usage
		hasUsage = true
	}

	readFiles, modifiedFiles := ComputeFileLists(preparation.FileOps)
	summary += FormatFileOperations(readFiles, modifiedFiles)

	if preparation.FirstKeptEntryID == "" {
		return nil, fmt.Errorf("First kept entry has no UUID - session may need migration")
	}

	details, _ := ai.MarshalJSON(CompactionDetails{ReadFiles: readFiles, ModifiedFiles: modifiedFiles})
	result := &CompactionResult{
		Summary: summary, FirstKeptEntryID: preparation.FirstKeptEntryID,
		TokensBefore: int64(preparation.TokensBefore),
		Details:      details,
	}
	if hasUsage {
		u := summaryUsage
		result.Usage = &u
	}
	return result, nil
}

// summaryWithUsage is the generateSummaryWithUsage outcome.
type summaryWithUsage struct {
	text  string
	usage *ai.Usage
}

// maxSummarizationRequests bounds the requests one summarization may spend when
// the answer does not fit: each extra pass re-sends conversation text, so the
// escalation stops and reports the failure instead of looping.
const maxSummarizationRequests = 10

// summarizationPlan is the target of one summarization: its instructions, the
// label its failures carry, and the first attempt's budget.
type summarizationPlan struct {
	label string
	// prompt renders the request for one chunk. previousSummary is empty only
	// for the first chunk of a summary that starts from nothing.
	prompt    func(messages []ai.Message, previousSummary string) string
	reasoning ai.ThinkingLevel
}

// summarizationEscalation carries the request budget and the accumulated usage
// of one summarization across its passes.
type summarizationEscalation struct {
	requests int
	usage    ai.Usage
}

// generateSummaryWithUsage generates or updates a conversation summary. The
// request carries no output limit: the provider sizes the answer from the actual
// request, so consistency with normal turns comes for free and reasoning tokens
// cannot starve the summary (upstream v1.4.0 6fcb48670; reserveTokens only
// decides when compaction starts).
func generateSummaryWithUsage(currentMessages []ai.Message, options CompactionOptions, previousSummary string) (*summaryWithUsage, error) {
	return summarizeConversation(currentMessages, options, previousSummary, summarizationPlan{
		label: "Summarization",
		prompt: func(messages []ai.Message, previous string) string {
			return conversationSummaryPrompt(messages, options, previous)
		},
		reasoning: options.ThinkingLevel,
	})
}

// generateTurnPrefixSummary summarizes a split turn's prefix, under the same
// /compact instructions as the conversation summary: a split turn can be all the
// new content since the last compaction, so dropping them there would drop them
// entirely (upstream v1.4.0 adfc14461).
func generateTurnPrefixSummary(messages []ai.Message, options CompactionOptions, customInstructions string) (*summaryWithUsage, error) {
	return summarizeConversation(messages, options, "", summarizationPlan{
		label: "Turn prefix summarization",
		prompt: func(messages []ai.Message, previous string) string {
			return turnPrefixSummaryPrompt(messages, previous, customInstructions)
		},
		reasoning: options.ThinkingLevel,
	})
}

// conversationSummaryPrompt renders the conversation summary request, port of
// generateSummaryWithUsage's prompt assembly.
func conversationSummaryPrompt(messages []ai.Message, options CompactionOptions, previousSummary string) string {
	conversationText := SerializeConversation(ConvertToLlm(messages))
	promptText := fmt.Sprintf("<conversation>\n%s\n</conversation>\n\n", conversationText)
	if previousSummary != "" {
		promptText += fmt.Sprintf("<previous-summary>\n%s\n</previous-summary>\n\n", previousSummary)
	}
	basePrompt := summarizationPrompt
	if previousSummary != "" {
		basePrompt = updateSummarizationPrompt
	}
	if options.CustomInstructions != "" {
		basePrompt = fmt.Sprintf("%s\n\nAdditional focus: %s", basePrompt, options.CustomInstructions)
	}
	return promptText + basePrompt
}

// turnPrefixSummaryPrompt renders the split-turn prompt. A later chunk merges
// into the checkpoint so far, so the sections stay one list instead of two
// concatenated ones.
func turnPrefixSummaryPrompt(messages []ai.Message, previousSummary, customInstructions string) string {
	conversationText := SerializeConversation(ConvertToLlm(messages))
	var promptText string
	if previousSummary == "" {
		promptText = fmt.Sprintf("# Conversation\n%s\n\n# Instructions\n%s", conversationText, turnPrefixSummarizationPrompt)
	} else {
		promptText = fmt.Sprintf("# Conversation\n%s\n\n<previous-summary>\n%s\n</previous-summary>\n\n# Instructions\n%s\n\n%s",
			conversationText, previousSummary, turnPrefixMergeInstruction, turnPrefixSummarizationPrompt)
	}
	if customInstructions != "" {
		promptText = fmt.Sprintf("%s\n\nAdditional focus: %s", promptText, customInstructions)
	}
	return promptText
}

// turnPrefixMergeInstruction keeps a merged turn-prefix checkpoint in its own
// shape. The turn-prefix sections (Original Request, Progress So Far, Context
// Needed to Continue) differ from the conversation ones, so the conversation
// update prompt cannot be reused here: it would restyle a checkpoint that
// compact() prefixes as "* Turn prefix: ...".
const turnPrefixMergeInstruction = `The summary in <previous-summary> was produced from earlier messages of this same turn. Merge it with the messages above: keep everything it established, add what these messages show, and use exactly the sections below.`

// summarizeConversation summarizes messages under one plan, escalating until the
// answer fits (port of generateSummaryWithUsage with D264's escalation).
func summarizeConversation(messages []ai.Message, options CompactionOptions, previousSummary string, plan summarizationPlan) (*summaryWithUsage, error) {
	// One summarization keeps one routing id for all of its passes, the way
	// upstream's completeSummarization generates the id once and its retries reuse
	// it, so escalated passes are recognizable as one request group.
	if options.SessionID == "" {
		options.SessionID = UUIDv7()
	}
	escalation := &summarizationEscalation{}
	summary, err := summarizeChunks(options.Model, messages, options, plan, previousSummary, plan.reasoning, escalation)
	if err != nil {
		return nil, err
	}
	summary.usage = &escalation.usage
	return summary, nil
}

// summarizeChunks summarizes one chunk of messages. A length stop holds partial
// text and cannot become a checkpoint, so the budget it ran into is resolved
// before the request is worth repeating: first with the same input and more room
// (the budget is what the window had left, and reasoning tokens come out of that
// same budget), then conclusively by summarizing less input at a time and
// merging each chunk into the summary so far. Splits keep a tool call with its
// result. Upstream fails the compaction on the first length stop (D264).
func summarizeChunks(
	model *ai.Model,
	messages []ai.Message,
	options CompactionOptions,
	plan summarizationPlan,
	previousSummary string,
	reasoning ai.ThinkingLevel,
	escalation *summarizationEscalation,
) (*summaryWithUsage, error) {
	if len(messages) == 0 {
		return &summaryWithUsage{text: previousSummary}, nil
	}
	if escalation.requests >= maxSummarizationRequests {
		// Every chunk that reached this point already ran out of answer room, so
		// a further pass would only spend another full conversation send.
		return nil, summarizationLengthFailure(plan.label)
	}
	context := summarizationContext(plan.prompt(messages, previousSummary))
	response, err := requestSummarization(model, context, options, summarizationReasoningEnabled(model, reasoning), reasoning, escalation)
	if err != nil {
		return nil, err
	}
	if response.StopReason != ai.StopLength {
		return summarizationResult(plan.label, response)
	}
	// Same input, without the thinking tokens competing for the answer: the
	// provider already sized the budget from the request, so only the reasoning
	// share is left to free.
	if summarizationReasoningEnabled(model, reasoning) {
		response, err = requestSummarization(model, context, options, false, ai.ThinkOff, escalation)
		if err != nil {
			return nil, err
		}
		if response.StopReason != ai.StopLength {
			return summarizationResult(plan.label, response)
		}
		reasoning = ai.ThinkOff
	}
	// Conclusive step: less input at a time, so each chunk's answer has the whole
	// budget. One message that still overflows is a real failure, not a budget
	// problem. A chunk that still overflows splits again.
	if len(messages) <= 1 {
		return nil, summarizationLengthFailure(plan.label)
	}
	split := summarizationSplitPoint(messages)
	if split <= 0 || split >= len(messages) {
		return nil, summarizationLengthFailure(plan.label)
	}
	first, err := summarizeChunks(model, messages[:split], options, plan, previousSummary, reasoning, escalation)
	if err != nil {
		return nil, err
	}
	return summarizeChunks(model, messages[split:], options, plan, first.text, reasoning, escalation)
}

// summarizationContext wraps a prompt in the summarization request.
func summarizationContext(promptText string) ai.TranscriptContext {
	return ai.NormalizeContext(ai.Context{
		SystemPrompt: strPtrOf(SummarizationSystemPrompt),
		Messages: []ai.Message{&ai.UserMessage{
			Content:   ai.StringOrBlocks{Blocks: ai.ContentList{ai.TextContent{Text: promptText}}},
			Timestamp: time.Now().UnixMilli(),
		}},
	})
}

// summarizationReasoningEnabled reports whether the request carries a thinking
// level (upstream createSummarizationOptions sets reasoning only for a
// reasoning-capable model and a selected level).
func summarizationReasoningEnabled(model *ai.Model, reasoning ai.ThinkingLevel) bool {
	return model.Reasoning && reasoning != "" && reasoning != ai.ThinkOff
}

// summarizationSplitPoint splits messages near the middle by estimated tokens so
// both halves fit where the whole input did not, and never between a tool call
// and its result: a chunk has to read on its own, and a call without its answer
// (or an answer without its call) summarizes worse than a slightly uneven split
// (D264).
func summarizationSplitPoint(messages []ai.Message) int {
	target := summarizationWeightMidpoint(messages)
	outstanding := map[string]struct{}{}
	best := 0
	for boundary := 1; boundary < len(messages); boundary++ {
		summarizationTrackToolCalls(messages[boundary-1], outstanding)
		if len(outstanding) > 0 {
			// A call issued on the left is still waiting for a result on the
			// right, so this boundary would separate the pair.
			continue
		}
		if best == 0 || absInt(boundary-target) < absInt(best-target) {
			best = boundary
		}
	}
	if best == 0 {
		// Every boundary sits inside a call/result chain; a separated pair still
		// beats failing the compaction, so keep the weight split inside the
		// input even when the final message alone crosses half the weight.
		return min(target, len(messages)-1)
	}
	return best
}

// summarizationWeightMidpoint is the weight-only split: the first boundary at
// which the left side reaches half of the estimated tokens.
func summarizationWeightMidpoint(messages []ai.Message) int {
	total := int64(0)
	for _, message := range messages {
		total += int64(EstimateAgentMessageTokens(message))
	}
	half := total / 2
	sum := int64(0)
	for index, message := range messages {
		sum += int64(EstimateAgentMessageTokens(message))
		if sum >= half {
			return index + 1
		}
	}
	return len(messages)
}

// summarizationTrackToolCalls folds one message into the outstanding-call set: an
// assistant message opens its tool calls, and a tool result closes its own. A
// result whose call was summarized earlier closes nothing.
func summarizationTrackToolCalls(message ai.Message, outstanding map[string]struct{}) {
	switch m := message.(type) {
	case *ai.AssistantMessage:
		for _, block := range m.Content {
			if call, ok := block.(ai.ToolCall); ok && call.ID != "" {
				outstanding[call.ID] = struct{}{}
			}
		}
	case *ai.ToolResultMessage:
		delete(outstanding, m.ToolCallID)
	}
}

// absInt is |value|.
func absInt(value int) int {
	if value < 0 {
		return -value
	}
	return value
}

// summarizationResult turns a final summarization response into a summary,
// rejecting the responses that cannot become a checkpoint.
func summarizationResult(label string, response *ai.AssistantMessage) (*summaryWithUsage, error) {
	if failure := GetSummarizationFailure(response, label); failure != "" {
		return nil, fmt.Errorf("%s", failure)
	}
	for _, block := range response.Content {
		if _, ok := block.(ai.ToolCall); ok {
			return nil, fmt.Errorf("%s attempted to call a tool", label)
		}
	}
	return &summaryWithUsage{text: contentTextOf(response.Content)}, nil
}

// summarizationLengthFailure is upstream's message for a summary that never fit,
// built from the same helper so the wording stays single-sourced.
func summarizationLengthFailure(label string) error {
	return fmt.Errorf("%s", GetSummarizationFailure(&ai.AssistantMessage{StopReason: ai.StopLength}, label))
}

// requestSummarization issues one summarization request through the
// retry-wrapped choke point and accounts its usage.
func requestSummarization(
	model *ai.Model,
	context ai.TranscriptContext,
	options CompactionOptions,
	reasoningEnabled bool,
	reasoning ai.ThinkingLevel,
	escalation *summarizationEscalation,
) (*ai.AssistantMessage, error) {
	streamOptions := ai.SimpleStreamOptions{
		StreamOptions: ai.StreamOptions{
			APIKey:    options.APIKey,
			Ctx:       options.Ctx,
			Env:       ai.ProviderEnv(options.Env),
			SessionID: options.SessionID,
			Headers:   stringHeaders(options.Headers),
		},
	}
	if reasoningEnabled {
		streamOptions.Reasoning = reasoning
	}
	escalation.requests++
	response, err := CompleteSummarization(model, context, &streamOptions, options.StreamFn, options.Retry, options.Callbacks)
	if response != nil {
		escalation.usage = CombineUsage(escalation.usage, response.Usage)
	}
	return response, err
}

func strPtrOf(s string) *string { return &s }

// contentTextOf joins assistant content text with newlines.
func contentTextOf(content ai.ContentList) string {
	var texts []string
	for _, block := range content {
		if tc, ok := block.(ai.TextContent); ok {
			texts = append(texts, tc.Text)
		}
	}
	return strings.Join(texts, "\n")
}

// Local aliases to the ai package's JS-semantics helpers.
// sortStrings orders strings by UTF-16 code units (JavaScript's default string
// ordering, upstream computeFileLists' Array.prototype.sort). The surrogate
// range sits below the last BMP scalars, so a supplementary scalar sorts
// before scalars like U+E000, unlike Go's UTF-8 byte comparison.
func sortStrings(list []string) {
	for i := 1; i < len(list); i++ {
		for j := i; j > 0 && compareUTF16(list[j], list[j-1]) < 0; j-- {
			list[j], list[j-1] = list[j-1], list[j]
		}
	}
}

// compareUTF16 compares two strings by their UTF-16 code-unit sequences.
// Equal decoded runes carry equal unit sequences, so a rune walk decides at
// the first differing rune; its two units are compared in order (a decoded Go
// rune never encodes to a lone surrogate, so a one-unit rune cannot prefix a
// two-unit one).
func compareUTF16(a, b string) int {
	indexA, indexB := 0, 0
	for indexA < len(a) && indexB < len(b) {
		runeA, sizeA := utf8.DecodeRuneInString(a[indexA:])
		runeB, sizeB := utf8.DecodeRuneInString(b[indexB:])
		if runeA != runeB {
			if first := cmp.Compare(utf16Unit(runeA, 0), utf16Unit(runeB, 0)); first != 0 {
				return first
			}
			return cmp.Compare(utf16Unit(runeA, 1), utf16Unit(runeB, 1))
		}
		indexA += sizeA
		indexB += sizeB
	}
	switch {
	case indexA < len(a):
		return 1
	case indexB < len(b):
		return -1
	default:
		return 0
	}
}

// utf16Unit returns the first (index 0) or second (index 1) UTF-16 code unit
// of r; a BMP scalar has no second unit, which no comparison can reach.
func utf16Unit(r rune, index int) uint16 {
	if r < 0x10000 {
		if index == 0 {
			return uint16(r)
		}
		return 0
	}
	shifted := r - 0x10000
	if index == 0 {
		return uint16(0xD800 + shifted>>10)
	}
	return uint16(0xDC00 + shifted&0x3FF)
}

func JSLength(s string) int { return ai.JSLength(s) }

func JSSlice(s string, start, end int) string { return ai.JSSlice(s, start, end) }

func ceilDiv(a, b int) int { return (a + b - 1) / b }
