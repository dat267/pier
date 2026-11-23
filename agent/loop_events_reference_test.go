package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/dat267/pier/ai"
)

// Reference: pi v1.5.2 (b4afdddf), packages/agent/src/agent-loop.ts
// runAgentLoop. testdata/loop-events/golden.json is produced by running the
// reference loop over scripted stream functions and plain tools, so this test
// compares the port's event sequence and appended messages against the
// reference instead of against expectations written by hand. Timestamps,
// durations and costs are dropped as timing artifacts.
func TestRunAgentLoopEventsMatchReference(t *testing.T) {
	data, err := os.ReadFile("testdata/loop-events/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Name     string `json:"name"`
		Events   []any  `json:"events"`
		Messages []any  `json:"messages"`
	}
	if err := json.Unmarshal(data, &fixtures); err != nil {
		t.Fatal(err)
	}
	if len(fixtures) == 0 {
		t.Fatal("the loop-events corpus is empty")
	}
	for _, fixture := range fixtures {
		t.Run(fixture.Name, func(t *testing.T) {
			events, messages := runLoopScenario(t, loopScenarioFor(fixture.Name))
			assertProjectionEqual(t, "events", sortParallelToolEnds(events), sortParallelToolEnds(fixture.Events))
			assertProjectionEqual(t, "messages", messages, fixture.Messages)
		})
	}
}

// sortParallelToolEnds orders a run of adjacent tool_execution_end events by
// tool call id. A parallel batch reports each call's end when it finishes, so
// the order inside one batch is completion order: the reference's microtask
// scheduling happens to finish instant calls in call order, while the port's
// goroutines do not. Sequential batches interleave ends with message events, so
// their order is still compared.
func sortParallelToolEnds(events []any) []any {
	sorted := make([]any, len(events))
	copy(sorted, events)
	for start := 0; start < len(sorted); {
		end := start
		for end < len(sorted) && eventTypeOf(sorted[end]) == string(ToolExecutionEnd) {
			end++
		}
		if end-start > 1 {
			run := sorted[start:end]
			slices.SortFunc(run, func(a, b any) int {
				return strings.Compare(fmt.Sprint(a.(map[string]any)["toolCallId"]), fmt.Sprint(b.(map[string]any)["toolCallId"]))
			})
		}
		start = max(end, start+1)
	}
	return sorted
}

func eventTypeOf(event any) string {
	projected, _ := event.(map[string]any)
	name, _ := projected["type"].(string)
	return name
}

// assertProjectionEqual compares the two projections as canonical JSON: the
// golden decodes into []any and float64 where the port builds []string and int,
// and encoding/json sorts map keys, so equal values encode identically.
func assertProjectionEqual(t *testing.T, label string, got, want any) {
	t.Helper()
	gotJSON, err := json.MarshalIndent(got, "", " ")
	if err != nil {
		t.Fatal(err)
	}
	wantJSON, err := json.MarshalIndent(want, "", " ")
	if err != nil {
		t.Fatal(err)
	}
	if string(gotJSON) == string(wantJSON) {
		return
	}
	t.Fatalf("%s differ from the reference\ngot:\n%s\nwant:\n%s", label, gotJSON, wantJSON)
}

// loopToolSpec is one tool the scenarios declare.
type loopToolSpec struct {
	name     string
	behavior string // "", "boom" or "stop"
	mode     ToolExecutionMode
}

// loopTurn is one scripted stream response.
type loopTurn struct {
	chunks     []string
	text       string
	toolCalls  []ai.ToolCall
	stopReason ai.StopReason
	steer      string
}

// loopScenario is one scripted run.
type loopScenario struct {
	prompt  string
	tools   []loopToolSpec
	turns   []loopTurn
	block   string
	rewrite bool
}

func loopScenarioFor(name string) loopScenario {
	echo := func(id, toolName string, value int) ai.ToolCall {
		return ai.ToolCall{ID: id, Name: toolName, Arguments: json.RawMessage(fmt.Sprintf(`{"v":%d}`, value))}
	}
	switch name {
	case "text-turn":
		return loopScenario{
			prompt: "hi",
			turns:  []loopTurn{{chunks: []string{"he", "llo"}, stopReason: ai.StopStop}},
		}
	case "parallel-batch":
		return loopScenario{
			prompt: "run",
			tools:  []loopToolSpec{{name: "a"}, {name: "b"}},
			turns: []loopTurn{
				{toolCalls: []ai.ToolCall{echo("c1", "a", 1), echo("c2", "b", 2)}, stopReason: ai.StopToolUse},
				{text: "done", stopReason: ai.StopStop},
			},
		}
	case "sequential-batch":
		return loopScenario{
			prompt: "run",
			tools:  []loopToolSpec{{name: "a"}, {name: "b", mode: ToolExecutionSequential}},
			turns: []loopTurn{
				{toolCalls: []ai.ToolCall{echo("c1", "a", 1), echo("c2", "b", 2)}, stopReason: ai.StopToolUse},
				{text: "done", stopReason: ai.StopStop},
			},
		}
	case "terminate-batch":
		return loopScenario{
			prompt: "stop",
			tools:  []loopToolSpec{{name: "stop", behavior: "stop"}},
			turns: []loopTurn{
				{toolCalls: []ai.ToolCall{echo("c1", "stop", 1), echo("c2", "stop", 2)}, stopReason: ai.StopToolUse},
			},
		}
	case "length-stop":
		return loopScenario{
			prompt: "long",
			tools:  []loopToolSpec{{name: "a"}},
			turns: []loopTurn{
				{toolCalls: []ai.ToolCall{echo("c1", "a", 1)}, stopReason: ai.StopLength},
			},
		}
	case "unknown-tool-and-throw":
		return loopScenario{
			prompt: "mixed",
			tools:  []loopToolSpec{{name: "boom", behavior: "boom"}},
			turns: []loopTurn{
				{toolCalls: []ai.ToolCall{
					{ID: "c1", Name: "missing", Arguments: json.RawMessage(`{}`)},
					{ID: "c2", Name: "boom", Arguments: json.RawMessage(`{}`)},
				}, stopReason: ai.StopToolUse},
				{text: "after", stopReason: ai.StopStop},
			},
		}
	case "blocked-call":
		return loopScenario{
			prompt: "block",
			tools:  []loopToolSpec{{name: "a"}},
			block:  "a",
			turns: []loopTurn{
				{toolCalls: []ai.ToolCall{echo("c1", "a", 1)}, stopReason: ai.StopToolUse},
				{text: "after", stopReason: ai.StopStop},
			},
		}
	case "after-rewrite":
		return loopScenario{
			prompt:  "rewrite",
			tools:   []loopToolSpec{{name: "a"}},
			rewrite: true,
			turns: []loopTurn{
				{toolCalls: []ai.ToolCall{echo("c1", "a", 1)}, stopReason: ai.StopToolUse},
				{text: "after", stopReason: ai.StopStop},
			},
		}
	case "steering":
		return loopScenario{
			prompt: "first",
			turns: []loopTurn{
				{text: "one", stopReason: ai.StopStop, steer: "typed while waiting"},
				{text: "two", stopReason: ai.StopStop},
			},
		}
	}
	panic("unknown loop scenario " + name)
}

func runLoopScenario(t *testing.T, scenario loopScenario) ([]any, []any) {
	t.Helper()
	ctx := context.Background()
	model := &ai.Model{ID: "m", API: "test", Provider: "test"}

	tools := make([]AgentTool, 0, len(scenario.tools))
	for _, spec := range scenario.tools {
		tool := AgentTool{
			Name:        spec.name,
			Description: spec.name,
			Parameters:  json.RawMessage(`{"type":"object","properties":{"v":{"type":"number"}}}`),
			Execute: func(id string, args json.RawMessage, _ context.Context, _ func(AgentToolResult)) (AgentToolResult, error) {
				if spec.behavior == "boom" {
					return AgentToolResult{}, fmt.Errorf("boom")
				}
				var decoded struct {
					V int `json:"v"`
				}
				_ = json.Unmarshal(args, &decoded)
				return AgentToolResult{
					Content:   []ai.Content{ai.TextContent{Text: fmt.Sprintf("%s:%d", spec.name, decoded.V)}},
					Details:   json.RawMessage(fmt.Sprintf(`{"by":%q}`, spec.name)),
					Terminate: spec.behavior == "stop",
				}, nil
			},
		}
		if spec.mode != "" {
			tool.ExecutionMode = spec.mode
		}
		tools = append(tools, tool)
	}

	var steering []ai.Message
	streamCalls := 0
	streamFn := func(_ *ai.Model, _ ai.TranscriptContext, _ *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
		turn := loopTurn{text: "fallback", stopReason: ai.StopStop}
		if streamCalls < len(scenario.turns) {
			turn = scenario.turns[streamCalls]
		}
		streamCalls++
		stream := ai.NewAssistantMessageEventStream()
		partial := loopAssistantMessage(nil, turn.stopReason)
		stream.Push(ai.AssistantMessageEvent{Type: ai.EventStart, Partial: partial})
		chunks := turn.chunks
		if len(chunks) == 0 && turn.text != "" {
			chunks = []string{turn.text}
		}
		text := ""
		for _, chunk := range chunks {
			text += chunk
			stream.Push(ai.AssistantMessageEvent{
				Type: ai.EventTextDelta, ContentIndex: 0, Delta: chunk,
				Partial: loopAssistantMessage([]ai.Content{ai.TextContent{Text: text}}, turn.stopReason),
			})
		}
		content := []ai.Content{}
		if turn.text != "" {
			text = turn.text
		}
		if text != "" {
			content = append(content, ai.TextContent{Text: text})
		}
		for _, call := range turn.toolCalls {
			content = append(content, call)
		}
		final := loopAssistantMessage(content, turn.stopReason)
		stream.Push(ai.AssistantMessageEvent{Type: ai.EventDone, Reason: turn.stopReason, Message: final})
		stream.End(&final)
		if turn.steer != "" {
			steering = append(steering, &ai.UserMessage{
				Content: ai.StringOrBlocks{Blocks: []ai.Content{ai.TextContent{Text: turn.steer}}}, Timestamp: 2,
			})
		}
		return stream
	}

	config := &AgentLoopConfig{
		Model:         model,
		ConvertToLlm:  func(messages []ai.Message) []ai.Message { return messages },
		ToolExecution: ToolExecutionParallel,
		GetSteeringMessages: func(context.Context) ([]ai.Message, error) {
			queued := steering
			steering = nil
			return queued, nil
		},
	}
	if scenario.block != "" {
		blocked := scenario.block
		config.BeforeToolCall = func(call *BeforeToolCallContext, _ context.Context) (*BeforeToolCallResult, error) {
			if call.ToolCall.Name == blocked {
				return &BeforeToolCallResult{Block: true, Reason: "blocked " + blocked}, nil
			}
			return nil, nil
		}
	}
	if scenario.rewrite {
		yes := true
		config.AfterToolCall = func(*AfterToolCallContext, context.Context) (*AfterToolCallResult, error) {
			return &AfterToolCallResult{
				Content:    []ai.Content{ai.TextContent{Text: "rewritten"}},
				HasContent: true,
				Details:    json.RawMessage(`{"rewritten":true}`),
				HasDetails: true,
				IsError:    &yes,
			}, nil
		}
	}

	var events []any
	emit := func(event AgentEvent) error {
		events = append(events, projectLoopEvent(event))
		return nil
	}
	prompts := []ai.Message{&ai.UserMessage{
		Content: ai.StringOrBlocks{Blocks: []ai.Content{ai.TextContent{Text: scenario.prompt}}}, Timestamp: 0,
	}}
	messages := RunAgentLoop(prompts, AgentContext{Tools: tools}, config, ctx, emit, streamFn)
	projected := make([]any, 0, len(messages))
	for _, message := range messages {
		projected = append(projected, projectLoopMessage(message))
	}
	return events, projected
}

func loopAssistantMessage(content []ai.Content, stopReason ai.StopReason) *ai.AssistantMessage {
	if content == nil {
		content = []ai.Content{}
	}
	return &ai.AssistantMessage{
		Content: content, API: "test", Provider: "test", Model: "m", StopReason: stopReason, Timestamp: 1,
	}
}

func projectLoopMessage(message ai.Message) any {
	if message == nil {
		return nil
	}
	out := map[string]any{"role": string(ai.RoleOf(message))}
	switch typed := message.(type) {
	case *ai.UserMessage:
		out["content"] = projectLoopContent(typed.Content.Blocks)
	case *ai.AssistantMessage:
		out["content"] = projectLoopContent(typed.Content)
		out["stopReason"] = string(typed.StopReason)
		if typed.ErrorMessage != nil {
			out["errorMessage"] = *typed.ErrorMessage
		}
	case *ai.ToolResultMessage:
		content := make([]ai.Content, 0, len(typed.Content))
		for _, block := range typed.Content {
			content = append(content, block)
		}
		out["content"] = projectLoopContent(content)
		out["toolCallId"] = typed.ToolCallID
		out["toolName"] = typed.ToolName
		out["isError"] = typed.IsError
	case *ai.SystemMessage:
		blocks := make([]ai.Content, 0, 1)
		if typed.Content.String() {
			blocks = append(blocks, ai.TextContent{Text: typed.Content.Text})
		} else {
			blocks = append(blocks, typed.Content.Blocks...)
		}
		out["content"] = projectLoopContent(blocks)
		if len(typed.ToolsAdded) > 0 {
			names := make([]string, 0, len(typed.ToolsAdded))
			for _, tool := range typed.ToolsAdded {
				names = append(names, tool.Name)
			}
			out["toolsAdded"] = names
		}
		if len(typed.ToolsRemoved) > 0 {
			names := make([]string, 0, len(typed.ToolsRemoved))
			for _, tool := range typed.ToolsRemoved {
				names = append(names, tool.Name)
			}
			out["toolsRemoved"] = names
		}
	}
	return out
}

func projectLoopContent(content []ai.Content) []any {
	out := make([]any, 0, len(content))
	for _, block := range content {
		switch typed := block.(type) {
		case ai.TextContent:
			out = append(out, map[string]any{"type": "text", "text": typed.Text})
		case ai.ToolCall:
			var arguments any
			_ = json.Unmarshal(typed.Arguments, &arguments)
			out = append(out, map[string]any{
				"type": "toolCall", "id": typed.ID, "name": typed.Name, "arguments": arguments,
			})
		default:
			out = append(out, map[string]any{"type": "other"})
		}
	}
	return out
}

func projectLoopEvent(event AgentEvent) any {
	out := map[string]any{"type": string(event.Type)}
	switch event.Type {
	case MessageStart, MessageEnd:
		out["message"] = projectLoopMessage(event.Message)
	case MessageUpdate:
		out["update"] = string(event.AssistantMessageEvent.Type)
		if event.AssistantMessageEvent.Delta != "" {
			out["delta"] = event.AssistantMessageEvent.Delta
		}
		out["message"] = projectLoopMessage(event.Message)
	case ToolExecutionStart:
		out["toolCallId"] = event.ToolCallID
		out["toolName"] = event.ToolName
		var args any
		_ = json.Unmarshal(event.Args, &args)
		out["args"] = args
	case ToolExecutionEnd:
		out["toolCallId"] = event.ToolCallID
		out["toolName"] = event.ToolName
		out["isError"] = event.IsError
		out["content"] = projectLoopContent(event.Result.Content)
		var details any
		if len(event.Result.Details) > 0 {
			_ = json.Unmarshal(event.Result.Details, &details)
		}
		out["details"] = details
		out["terminate"] = event.Result.Terminate
		out["hasDuration"] = event.DurationMs != nil
	case TurnEnd:
		out["message"] = projectLoopMessage(event.Message)
		results := make([]any, 0, len(event.ToolResults))
		for _, result := range event.ToolResults {
			results = append(results, projectLoopMessage(result))
		}
		out["toolResults"] = results
	case AgentEnd:
		messages := make([]any, 0, len(event.Messages))
		for _, message := range event.Messages {
			messages = append(messages, projectLoopMessage(message))
		}
		out["messages"] = messages
	}
	return out
}
