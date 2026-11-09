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
