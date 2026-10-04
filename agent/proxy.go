package agent

// Port of packages/agent/src/proxy.ts: a stream function that routes model
// calls through a server which owns provider auth. The server strips the
// partial field from delta events to save bandwidth; the client rebuilds the
// assistant message from the events.

import (
	"bufio"
	"bytes"
	contextpkg "context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/dat267/pier/ai"
)

// ProxyStreamOptions configure StreamProxy (upstream ProxyStreamOptions). The
// embedded options carry the serializable request fields; local-only fields
// (Ctx, APIKey, ...) stay on the client.
type ProxyStreamOptions struct {
	ai.SimpleStreamOptions
	// AuthToken is the bearer token for the proxy server.
	AuthToken string
	// ProxyURL is the proxy server's base URL (e.g. "https://genai.example.com").
	ProxyURL string
}

// proxyRequestOptions is the serialized subset upstream sends.
type proxyRequestOptions struct {
	Temperature     *float64                   `json:"temperature,omitempty"`
	SamplingParams  map[string]json.RawMessage `json:"samplingParams,omitempty"`
	MaxTokens       *int                       `json:"maxTokens,omitempty"`
	Reasoning       string                     `json:"reasoning,omitempty"`
	CacheRetention  string                     `json:"cacheRetention,omitempty"`
	SessionID       string                     `json:"sessionId,omitempty"`
	Headers         map[string]string          `json:"headers,omitempty"`
	Metadata        map[string]json.RawMessage `json:"metadata,omitempty"`
	Transport       string                     `json:"transport,omitempty"`
	ThinkingBudgets *ai.ThinkingBudgets        `json:"thinkingBudgets,omitempty"`
	MaxRetryDelayMS *int                       `json:"maxRetryDelayMs,omitempty"`
}

type proxyRequestContext struct {
	Messages []json.RawMessage `json:"messages"`
}

type proxyRequest struct {
	Model   json.RawMessage     `json:"model"`
	Context proxyRequestContext `json:"context"`
	Options proxyRequestOptions `json:"options"`
}

// proxyWireEvent is one server event, with the partial field stripped.
type proxyWireEvent struct {
	Type                  string       `json:"type"`
	ContentIndex          int          `json:"contentIndex"`
	Delta                 string       `json:"delta"`
	ContentSignature      *string      `json:"contentSignature"`
	ID                    string       `json:"id"`
	ToolName              string       `json:"toolName"`
	ToolCall              *ai.ToolCall `json:"toolCall"`
	Reason                string       `json:"reason"`
	Usage                 *ai.Usage    `json:"usage"`
	ProviderThinkingLevel *string      `json:"providerThinkingLevel"`
	ErrorMessage          *string      `json:"errorMessage"`
}

func proxyRequestOptionsOf(options ProxyStreamOptions) proxyRequestOptions {
	stream := options.StreamOptions
	out := proxyRequestOptions{
		Temperature:     stream.Temperature,
		SamplingParams:  stream.SamplingParams,
		MaxTokens:       stream.MaxTokens,
		Reasoning:       options.Reasoning,
		CacheRetention:  stream.CacheRetention,
		SessionID:       stream.SessionID,
		Metadata:        stream.Metadata,
		Transport:       stream.Transport,
		ThinkingBudgets: options.ThinkingBudgets,
		MaxRetryDelayMS: stream.MaxRetryDelayMs,
	}
	if len(stream.Headers) > 0 {
		headers := map[string]string{}
		for key, value := range stream.Headers {
			if value != nil {
				headers[key] = *value
			}
		}
		if len(headers) > 0 {
			out.Headers = headers
		}
	}
	return out
}

func buildProxyRequest(model *ai.Model, context ai.TranscriptContext, options ProxyStreamOptions) ([]byte, error) {
	modelJSON, err := ai.MarshalJSON(model)
	if err != nil {
		return nil, err
	}
	messages, err := ai.MarshalMessages(context.Messages)
	if err != nil {
		return nil, err
	}
	return ai.MarshalJSON(proxyRequest{
		Model:   modelJSON,
		Context: proxyRequestContext{Messages: messages},
		Options: proxyRequestOptionsOf(options),
	})
}

// StreamProxy streams through a server instead of calling providers directly
// (upstream streamProxy). Use it as the StreamFn of an Agent that must route
// through a proxy.
func StreamProxy(model *ai.Model, context ai.TranscriptContext, options ProxyStreamOptions) *ai.AssistantMessageEventStream {
	stream := ai.NewAssistantMessageEventStream()
	go func() {
		partial := &ai.AssistantMessage{
			Content: ai.ContentList{}, StopReason: ai.StopPending,
			API: model.API, Provider: model.Provider, Model: model.ID,
			Usage: ai.Usage{Cost: ai.UsageCost{}}, Timestamp: time.Now().UnixMilli(),
		}
		ctx := options.Ctx
		if ctx == nil {
			ctx = contextpkg.Background()
		}
		// parseScratch accumulates toolcall_delta fragments per content index.
		parseScratch := map[int][]byte{}
		fail := func(reason ai.StopReason, message string) {
			partial.StopReason = reason
			partial.ErrorMessage = &message
			stream.Push(ai.AssistantMessageEvent{Type: ai.EventError, Reason: reason, Error: partial})
			stream.End(nil)
		}

		body, err := buildProxyRequest(model, context, options)
		if err != nil {
			fail(ai.StopError, err.Error())
			return
		}
		request, err := http.NewRequestWithContext(ctx, http.MethodPost,
			strings.TrimRight(options.ProxyURL, "/")+"/api/stream", bytes.NewReader(body))
		if err != nil {
			fail(ai.StopError, err.Error())
			return
		}
		request.Header.Set("Authorization", "Bearer "+options.AuthToken)
		request.Header.Set("Content-Type", "application/json")
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			if ctx.Err() != nil {
				fail(ai.StopAborted, "Request aborted by user")
				return
			}
			fail(ai.StopError, err.Error())
			return
		}
		defer response.Body.Close()
		if response.StatusCode >= 400 {
			message := fmt.Sprintf("Proxy error: %d %s", response.StatusCode, response.Status)
			if raw, readErr := io.ReadAll(io.LimitReader(response.Body, 1<<20)); readErr == nil {
				var errorData struct {
					Error string `json:"error"`
				}
				if json.Unmarshal(raw, &errorData) == nil && errorData.Error != "" {
					message = "Proxy error: " + errorData.Error
				}
			}
			fail(ai.StopError, message)
			return
		}

		sawTerminal := false
		processLine := func(line string) {
			if !strings.HasPrefix(line, "data: ") {
				return
			}
			data := strings.TrimSpace(line[len("data: "):])
			if data == "" {
				return
			}
			var wire proxyWireEvent
			if err := json.Unmarshal([]byte(data), &wire); err != nil {
				return
			}
			event := processProxyEvent(&wire, partial, parseScratch)
			if event == nil {
				return
			}
			if event.Type == ai.EventDone || event.Type == ai.EventError {
				sawTerminal = true
			}
			stream.Push(*event)
		}

		reader := bufio.NewReader(response.Body)
		for {
			line, readErr := reader.ReadString('\n')
			if len(line) > 0 {
				if ctx.Err() != nil {
					fail(ai.StopAborted, "Request aborted by user")
					return
				}
				processLine(strings.TrimRight(strings.TrimRight(line, "\n"), "\r"))
			}
			if readErr != nil {
				if readErr != io.EOF {
					fail(ai.StopError, readErr.Error())
					return
				}
				break
			}
		}
		if ctx.Err() != nil {
			fail(ai.StopAborted, "Request aborted by user")
			return
		}
		if !sawTerminal {
			// A clean EOF without a done/error event means the server dropped the
			// response mid-stream; surface it instead of leaving consumers waiting.
			fail(ai.StopError, "Connection closed by proxy server before the response completed")
			return
		}
		stream.End(nil)
	}()
	return stream
}

// processProxyEvent applies one server event to the partial message and returns
// the client event to push (upstream processProxyEvent).
func processProxyEvent(wire *proxyWireEvent, partial *ai.AssistantMessage, scratch map[int][]byte) *ai.AssistantMessageEvent {
	grow := func() {
		for len(partial.Content) <= wire.ContentIndex {
			partial.Content = append(partial.Content, nil)
		}
	}
	contentAt := func() ai.Content {
		grow()
		return partial.Content[wire.ContentIndex]
	}
	switch wire.Type {
	case "start":
		return &ai.AssistantMessageEvent{Type: ai.EventStart, Partial: partial}

	case "text_start":
		grow()
		partial.Content[wire.ContentIndex] = ai.TextContent{}
		return &ai.AssistantMessageEvent{Type: ai.EventTextStart, ContentIndex: wire.ContentIndex, Partial: partial}

	case "text_delta":
		content, ok := contentAt().(ai.TextContent)
		if !ok {
			return nil
		}
		content.Text += wire.Delta
		partial.Content[wire.ContentIndex] = content
		return &ai.AssistantMessageEvent{Type: ai.EventTextDelta, ContentIndex: wire.ContentIndex, Delta: wire.Delta, Partial: partial}

	case "text_end":
		content, ok := contentAt().(ai.TextContent)
		if !ok {
			return nil
		}
		if wire.ContentSignature != nil {
			content.TextSignature = wire.ContentSignature
		}
		partial.Content[wire.ContentIndex] = content
		return &ai.AssistantMessageEvent{Type: ai.EventTextEnd, ContentIndex: wire.ContentIndex, Content: content.Text, Partial: partial}

	case "thinking_start":
		grow()
		partial.Content[wire.ContentIndex] = ai.ThinkingContent{}
		return &ai.AssistantMessageEvent{Type: ai.EventThinkingStart, ContentIndex: wire.ContentIndex, Partial: partial}

	case "thinking_delta":
		content, ok := contentAt().(ai.ThinkingContent)
		if !ok {
			return nil
		}
		content.Thinking += wire.Delta
		partial.Content[wire.ContentIndex] = content
		return &ai.AssistantMessageEvent{Type: ai.EventThinkingDelta, ContentIndex: wire.ContentIndex, Delta: wire.Delta, Partial: partial}

	case "thinking_end":
		content, ok := contentAt().(ai.ThinkingContent)
		if !ok {
			return nil
		}
		if wire.ContentSignature != nil {
			content.ThinkingSignature = wire.ContentSignature
		}
		partial.Content[wire.ContentIndex] = content
		return &ai.AssistantMessageEvent{Type: ai.EventThinkingEnd, ContentIndex: wire.ContentIndex, Content: content.Thinking, Partial: partial}

	case "toolcall_start":
		grow()
		partial.Content[wire.ContentIndex] = ai.ToolCall{ID: wire.ID, Name: wire.ToolName, Arguments: json.RawMessage("{}")}
		scratch[wire.ContentIndex] = nil
		return &ai.AssistantMessageEvent{Type: ai.EventToolcallStart, ContentIndex: wire.ContentIndex, Partial: partial}

	case "toolcall_delta":
		content, ok := contentAt().(ai.ToolCall)
		if !ok {
			return nil
		}
		scratch[wire.ContentIndex] = append(scratch[wire.ContentIndex], wire.Delta...)
		content.Arguments = ai.ParseStreamingJSONText(string(scratch[wire.ContentIndex]))
		partial.Content[wire.ContentIndex] = content
		return &ai.AssistantMessageEvent{Type: ai.EventToolcallDelta, ContentIndex: wire.ContentIndex, Delta: wire.Delta, Partial: partial}

	case "toolcall_end":
		if _, ok := contentAt().(ai.ToolCall); !ok {
			return nil
		}
		if wire.ToolCall == nil {
			return nil
		}
		partial.Content[wire.ContentIndex] = *wire.ToolCall
		delete(scratch, wire.ContentIndex)
		return &ai.AssistantMessageEvent{Type: ai.EventToolcallEnd, ContentIndex: wire.ContentIndex, ToolCall: wire.ToolCall, Partial: partial}

	case "done":
		partial.StopReason = wire.Reason
		if wire.Usage != nil {
			partial.Usage = *wire.Usage
		}
		partial.ProviderThinkingLevel = wire.ProviderThinkingLevel
		return &ai.AssistantMessageEvent{Type: ai.EventDone, Reason: wire.Reason, Message: partial}

	case "error":
		partial.StopReason = wire.Reason
		partial.ErrorMessage = wire.ErrorMessage
		if wire.Usage != nil {
			partial.Usage = *wire.Usage
		}
		partial.ProviderThinkingLevel = wire.ProviderThinkingLevel
		return &ai.AssistantMessageEvent{Type: ai.EventError, Reason: wire.Reason, Error: partial}
	}
	return nil
}
