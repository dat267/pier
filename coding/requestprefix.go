package coding

import (
	"sync"

	"github.com/dat267/pier/ai"
)

// requestPrefixCapture remembers the last request the agent sent so that a
// compaction can summarize from it (D419). A provider caches the request
// prefix, so replaying that request with the summary instruction appended turns
// the compaction into a cache read instead of a cold prefill of a serialized
// copy of the same conversation.
//
// The capture is written on the agent's work goroutine and read by the
// compaction path, so it is guarded. Only the slice is copied: a message is
// immutable once it has been sent, so the compaction replays exactly the bytes
// the provider saw without deep-copying the transcript.
type requestPrefixCapture struct {
	mu     sync.Mutex
	prefix *CompactionPrefix
}

// record stores one outbound request.
func (c *requestPrefixCapture) record(model *ai.Model, transcript ai.TranscriptContext, options *ai.SimpleStreamOptions) {
	if c == nil || model == nil || len(transcript.Messages) == 0 {
		return
	}
	prefix := &CompactionPrefix{Model: model, Messages: append([]ai.Message{}, transcript.Messages...)}
	if options != nil {
		prefix.Reasoning = options.Reasoning
		prefix.CacheRetention = options.CacheRetention
	}
	c.mu.Lock()
	c.prefix = prefix
	c.mu.Unlock()
}

// snapshot returns the last captured request, or nil when there is none.
func (c *requestPrefixCapture) snapshot() *CompactionPrefix {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.prefix
}

// clear releases the captured transcript. A compaction has already spent the
// replay and installed a summary, so the messages it remembered are the
// pre-compaction history the session no longer keeps.
func (c *requestPrefixCapture) clear() {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.prefix = nil
	c.mu.Unlock()
}

// replayPrefix returns the request a compaction should replay: the captured one
// extended with the messages the turn appended after it, so the summary covers
// the session's whole context instead of stopping at the last request. The
// captured prefix itself is untouched, so the provider still serves it from its
// cache and only the appended tail is new input.
//
// It returns nil when the context no longer starts with the captured request,
// which is how a branch switch or a cleared session reads: replaying then would
// summarize a conversation this branch never had.
func (s *AgentSession) replayPrefix() *CompactionPrefix {
	captured := s.requestPrefix.snapshot()
	if captured == nil {
		return nil
	}
	context := s.convertToLlm(s.Agent.State().Messages)
	if len(context) < len(captured.Messages) {
		return nil
	}
	for index, message := range captured.Messages {
		if context[index] != message {
			return nil
		}
	}
	// A context that ends in an unanswered tool call cannot be sent as the last
	// message; the stock serialized prompt can.
	if endsWithToolCall(context[len(context)-1]) {
		return nil
	}
	extended := *captured
	extended.Messages = context
	return &extended
}

// llmConverter is the configured message converter, identity when unset.
func llmConverter(convert func([]ai.Message) []ai.Message) func([]ai.Message) []ai.Message {
	if convert == nil {
		return func(messages []ai.Message) []ai.Message { return messages }
	}
	return convert
}

// endsWithToolCall reports an assistant message that still expects tool results.
func endsWithToolCall(message ai.Message) bool {
	assistant, ok := message.(*ai.AssistantMessage)
	if !ok {
		return false
	}
	for _, block := range assistant.Content {
		if _, isCall := block.(ai.ToolCall); isCall {
			return true
		}
	}
	return false
}
