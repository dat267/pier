package ai

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"testing/iotest"
	"time"
)

// pi v1.4.0 api/anthropic-messages.ts uses one streaming TextDecoder for SSE,
// stripping a stream-leading BOM and replacing malformed UTF-8 subsequences.
func TestAnthropicSSEMatchesTextDecoder(t *testing.T) {
	for _, item := range []struct{ prefix, id, want string }{
		{"\ufeff", "id", "id"},
		{"", string([]byte{0xff, 0xff, 0xe2, 0x82}), "\ufffd\ufffd\ufffd"},
	} {
		body := item.prefix + "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"" + item.id + "\"}}\n\n" +
			"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"
		var id string
		count := 0
		err := IterateAnthropicEvents(context.Background(), iotest.OneByteReader(strings.NewReader(body)), func(event anthropicStreamEvent) {
			count++
			if event.Message != nil {
				id = event.Message.ID
			}
		})
		if err != nil || count != 2 || id != item.want {
			t.Errorf("prefix=%q id bytes=%x: count=%d id=%q error=%v, want two events and id=%q", item.prefix, []byte(item.id), count, id, err, item.want)
		}
	}
}

// The SDK completes a CR-terminated blank line immediately, without waiting
// for EOF or the next chunk to determine whether a following LF is present.
func TestOpenAICRFrameEmitsBeforeNextChunk(t *testing.T) {
	release := make(chan struct{})
	unblock := sync.OnceFunc(func() { close(release) })
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"hello\"},\"finish_reason\":null}]}\r\r"))
		w.(http.Flusher).Flush()
		<-release
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\r\rdata: [DONE]\r\r"))
	}))
	defer server.Close()
	defer unblock()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	model := testOpenAIModel()
	model.BaseURL = server.URL
	stream := StreamOpenAICompletions(model, NormalizeContext(Context{}), &OpenAICompletionsOptions{APIKey: "fixture", Ctx: ctx})
	gotDelta := false
	for {
		event, ok := stream.Next(ctx)
		if !ok {
			break
		}
		if event.Type == EventTextDelta && event.Delta == "hello" {
			gotDelta = true
			break
		}
	}
	if !gotDelta {
		t.Fatal("CR-only frame waited for another network chunk before emitting its delta")
	}
	unblock()
	if _, err := stream.Result(ctx); err != nil {
		t.Fatal(err)
	}
}

// pi v1.4.0 api/anthropic-messages.ts consumeLine accepts CR boundaries too.
func TestAnthropicAcceptsCRSeparatedSSEEvents(t *testing.T) {
	body := "event: message_start\rdata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg\",\"role\":\"assistant\",\"content\":[]}}\r\r" +
		"event: message_stop\rdata: {\"type\":\"message_stop\"}\r\r"
	var types []string
	err := IterateAnthropicEvents(context.Background(), strings.NewReader(body), func(event anthropicStreamEvent) { types = append(types, event.Type) })
	if err != nil || strings.Join(types, ",") != "message_start,message_stop" {
		t.Fatalf("event types=%q error=%v, want both CR-separated events", types, err)
	}
}

// pi v1.4.0 api/openai-responses.ts uses the same OpenAI SDK line decoder.
func TestResponsesAcceptsCRSeparatedSSEEvents(t *testing.T) {
	body := strings.Join([]string{
		`data: {"type":"response.output_item.added","output_index":0,"item":{"type":"message","id":"msg","role":"assistant"}}`, "",
		`data: {"type":"response.output_text.delta","output_index":0,"delta":"hello"}`, "",
		`data: {"type":"response.output_item.done","output_index":0,"item":{"type":"message","id":"msg","role":"assistant","content":[{"type":"output_text","text":"hello"}]}}`, "",
		`data: {"type":"response.completed","response":{"id":"resp","status":"completed"}}`, "", "",
	}, "\r")
	message := &AssistantMessage{}
	err := ProcessResponsesStream(context.Background(), iotest.OneByteReader(strings.NewReader(body)), message, NewAssistantMessageEventStream(), testResponsesModel(), &OpenAIResponsesOptions{}, nil)
	if err != nil || len(message.Content) != 1 || message.Content[0].(TextContent).Text != "hello" || message.StopReason != StopStop {
		t.Fatalf("message=%+v error=%v, want hello/stop from CR-only SSE", message, err)
	}
}

// pi v1.4.0 api/openai-completions.ts uses OpenAI 7.19.0's streaming decoder,
// whose LineDecoder supports CR, LF and CRLF. The actual SDK returned "hello"
// for this CR-only fixture; ordinary LF and CRLF are controls.
func TestOpenAICompletionsAcceptsSSELineEndings(t *testing.T) {
	for _, ending := range []string{"\r", "\n", "\r\n"} {
		body := strings.Join([]string{
			`data: {"choices":[{"delta":{"content":"hello"},"finish_reason":null}]}`,
			"",
			`data: {"choices":[{"delta":{},"finish_reason":"stop"}]}`,
			"", "data: [DONE]", "", "",
		}, ending)
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte(body))
		}))
		model := testOpenAIModel()
		model.BaseURL = server.URL
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		message, err := StreamOpenAICompletions(model, NormalizeContext(Context{}), &OpenAICompletionsOptions{APIKey: "fixture"}).Result(ctx)
		cancel()
		server.Close()
		if err != nil || len(message.Content) != 1 || message.Content[0].(TextContent).Text != "hello" || message.StopReason != StopStop {
			t.Errorf("line ending=%q: message=%+v error=%v, want hello/stop", ending, message, err)
		}
	}
}
