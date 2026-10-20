package ai

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// Upstream applies the settings-backed idle timeout to every provider request
// twice: core/http-dispatcher.ts sets undici's headersTimeout and bodyTimeout to
// it. The port arms the same timer before the request and re-arms it on every
// body read (D40, D254).

// stallServer answers every request the same way: sendHeaders controls whether
// the response headers arrive, and prelude is written and flushed before the
// handler stalls until the test ends.
func stallServer(t *testing.T, sendHeaders bool, prelude string) *httptest.Server {
	t.Helper()
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if sendHeaders {
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			if prelude != "" {
				_, _ = io.WriteString(w, prelude)
			}
			if flusher, ok := w.(http.Flusher); ok {
				flusher.Flush()
			}
		}
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(func() {
		close(release)
		server.Close()
	})
	return server
}

// awaitStreamError fails unless the stream settles with an error whose message
// contains want; the wait is bounded so a missing guard reports an abort instead
// of hanging.
func awaitStreamError(t *testing.T, stream *AssistantMessageEventStream, want string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	start := time.Now()
	message, err := stream.Result(ctx)
	if ctxErr(ctx) != nil || err != nil {
		t.Fatalf("stream did not settle: err=%v", err)
	}
	if message.StopReason != StopError {
		t.Fatalf("stop reason = %v; want error", message.StopReason)
	}
	if message.ErrorMessage == nil {
		t.Fatalf("missing error message; want %q", want)
	}
	if !strings.Contains(*message.ErrorMessage, want) {
		t.Fatalf("error message = %q; want %q", *message.ErrorMessage, want)
	}
	if elapsed := time.Since(start); elapsed > 4*time.Second {
		t.Fatalf("idle stream took %v to fail; want under 4s", elapsed)
	}
}

func anthropicIdleStream(model *Model, timeoutMS int) *AssistantMessageEventStream {
	return StreamAnthropic(model, NormalizeContext(Context{
		Messages: []Message{&UserMessage{Content: StringOrBlocks{Text: "hi"}, Timestamp: 1}},
	}), &AnthropicOptions{StreamOptions: StreamOptions{APIKey: "test-key", TimeoutMs: &timeoutMS}})
}

func TestStreamAnthropicAbortsWhenResponseHeadersIdle(t *testing.T) {
	model := testAnthropicModel()
	model.BaseURL = stallServer(t, false, "").URL
	awaitStreamError(t, anthropicIdleStream(model, 200), "headers idle")
}

func TestStreamAnthropicAbortsWhenStreamIdles(t *testing.T) {
	model := testAnthropicModel()
	model.BaseURL = stallServer(t, true, "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"model\":\"claude-opus-4-5\",\"usage\":{\"input_tokens\":10,\"output_tokens\":1}}}\n\n").URL
	awaitStreamError(t, anthropicIdleStream(model, 200), "stream idle")
}

// A stream that keeps producing data outlives the idle timeout: the timer bounds
// silence, not the lifetime of the request.
func TestStreamAnthropicKeepsAResponsiveStreamAlive(t *testing.T) {
	deltas := []string{"a", "b", "c", "d", "e", "f"}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		_, _ = io.WriteString(w, "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"model\":\"claude-opus-4-5\",\"usage\":{\"input_tokens\":10,\"output_tokens\":1}}}\n\n")
		flusher.Flush()
		_, _ = io.WriteString(w, "event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n")
		flusher.Flush()
		for _, delta := range deltas {
			time.Sleep(20 * time.Millisecond)
			_, _ = io.WriteString(w, "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\""+delta+"\"}}\n\n")
			flusher.Flush()
		}
		_, _ = io.WriteString(w, "event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":6}}\n\n")
		_, _ = io.WriteString(w, "event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
		flusher.Flush()
	}))
	defer server.Close()

	model := testAnthropicModel()
	model.BaseURL = server.URL
	// Two seconds of allowance for a 20ms cadence: only a stalled attempt to
	// read may trip the guard, never a slow scheduler.
	message, err := anthropicIdleStream(model, 2000).Result(context.Background())
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if message.StopReason != StopStop {
		t.Fatalf("stop reason = %v, message = %v", message.StopReason, *message.ErrorMessage)
	}
	if len(message.Content) != 1 {
		t.Fatalf("content = %+v", message.Content)
	}
	if text, ok := message.Content[0].(TextContent); !ok || text.Text != "abcdef" {
		t.Fatalf("content = %+v; want the complete stream", message.Content[0])
	}
}

// Every provider that streams over HTTP applies the settings-backed idle
// timeout to the response headers: a server that accepts the connection and
// never answers must fail the stream instead of hanging it.
func TestProviderStreamsAbortWhenResponseHeadersIdle(t *testing.T) {
	// Generous relative to machine speed: a slow or loaded runner must not turn a
	// timeout-mechanism test into a timing race.
	const timeoutMS = 200
	cases := []struct {
		name  string
		build func(t *testing.T, baseURL string) *AssistantMessageEventStream
	}{
		{"anthropic", func(t *testing.T, baseURL string) *AssistantMessageEventStream {
			model := testAnthropicModel()
			model.BaseURL = baseURL
			return anthropicIdleStream(model, timeoutMS)
		}},
		{"openai-completions", func(t *testing.T, baseURL string) *AssistantMessageEventStream {
			model := testOpenAIModel()
			model.BaseURL = baseURL
			return StreamOpenAICompletions(model, NormalizeContext(Context{
				Messages: []Message{&UserMessage{Content: StringOrBlocks{Text: "hi"}, Timestamp: 1}},
			}), &OpenAICompletionsOptions{StreamOptions: StreamOptions{APIKey: "test", TimeoutMs: intPtrT(timeoutMS)}})
		}},
		{"openai-responses", func(t *testing.T, baseURL string) *AssistantMessageEventStream {
			model := testResponsesModel()
			model.BaseURL = baseURL
			return StreamOpenAIResponses(model, NormalizeContext(Context{
				Messages: []Message{&UserMessage{Content: StringOrBlocks{Text: "hi"}, Timestamp: 1}},
			}), &OpenAIResponsesOptions{StreamOptions: StreamOptions{APIKey: "test", TimeoutMs: intPtrT(timeoutMS)}})
		}},
		{"pi-messages", func(t *testing.T, baseURL string) *AssistantMessageEventStream {
			return StreamPiMessages(piMessagesModel(baseURL), piMessagesContext(), &PiMessagesOptions{
				StreamOptions: StreamOptions{APIKey: "test", TimeoutMs: intPtrT(timeoutMS)},
			})
		}},
		{"mistral", func(t *testing.T, baseURL string) *AssistantMessageEventStream {
			return StreamMistralConversations(mistralModel(baseURL), mistralUserContext(), &MistralOptions{
				StreamOptions: StreamOptions{APIKey: "test", TimeoutMs: intPtrT(timeoutMS)},
			})
		}},
		{"google", func(t *testing.T, baseURL string) *AssistantMessageEventStream {
			model := testGoogleModel()
			model.BaseURL = baseURL
			return StreamGoogleGenerativeAI(model, NormalizeContext(Context{
				Messages: []Message{&UserMessage{Content: StringOrBlocks{Text: "hi"}, Timestamp: 1}},
			}), &GoogleOptions{StreamOptions: StreamOptions{APIKey: "test", TimeoutMs: intPtrT(timeoutMS)}})
		}},
		{"google-vertex", func(t *testing.T, baseURL string) *AssistantMessageEventStream {
			model := &Model{
				ID: "gemini-3-flash-preview", API: APIGoogleVertex, Provider: "google-vertex",
				BaseURL: baseURL, ContextWindow: 1000, MaxTokens: 100,
			}
			return StreamGoogleVertex(model, NormalizeContext(Context{
				Messages: []Message{&UserMessage{Content: StringOrBlocks{Text: "hi"}, Timestamp: 1}},
			}), &GoogleVertexOptions{
				GoogleOptions: GoogleOptions{StreamOptions: StreamOptions{APIKey: "test", TimeoutMs: intPtrT(timeoutMS)}},
				Project:       "test-project", Location: "us-central1",
			})
		}},
		{"bedrock", func(t *testing.T, baseURL string) *AssistantMessageEventStream {
			return StreamBedrockConverse(bedrockTestModel(baseURL), NormalizeContext(Context{
				Messages: []Message{&UserMessage{Content: StringOrBlocks{Text: "hi"}, Timestamp: 1}},
			}), &BedrockOptions{
				StreamOptions: StreamOptions{
					APIKey: "bearer-token", TimeoutMs: intPtrT(timeoutMS),
					Env: ProviderEnv{"AWS_ACCESS_KEY_ID": "AKID", "AWS_SECRET_ACCESS_KEY": "SECRET"},
				},
				BearerToken: "bearer-token",
			})
		}},
		{"openai-codex", func(t *testing.T, baseURL string) *AssistantMessageEventStream {
			return StreamOpenAICodexResponses(codexModel(baseURL), NormalizeContext(Context{
				Messages: []Message{&UserMessage{Content: StringOrBlocks{Text: "hi"}, Timestamp: 1}},
			}), &OpenAICodexResponsesOptions{
				StreamOptions: StreamOptions{APIKey: codexToken(t, "acct-1"), TimeoutMs: intPtrT(timeoutMS)},
				Transport:     "sse",
			})
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			want := "headers idle"
			switch tc.name {
			case "openai-codex":
				// Upstream keeps the codex-specific wording for this timeout.
				want = "Codex SSE response headers timed out"
			case "mistral":
				want = "Mistral response headers timed out"
			}
			server := stallServer(t, false, "")
			awaitStreamError(t, tc.build(t, server.URL), want)
		})
	}
}

// Upstream cleans up the codex headers timeout once the response arrives
// (api/openai-codex-responses.ts: the timeout signal wraps the fetch only), so
// an active SSE body keeps streaming. Cancelling the request context when the
// headers arrive would truncate it to whatever was already buffered.
func TestStreamCodexResponsesKeepsStreamingPastTheHeadersTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		_, _ = io.WriteString(w, `data: {"type":"response.created"}`+"\n\n")
		_, _ = io.WriteString(w, `data: {"type":"response.output_item.added","output_index":0,"item":{"type":"message","id":"m1","role":"assistant"}}`+"\n\n")
		_, _ = io.WriteString(w, `data: {"type":"response.output_text.delta","output_index":0,"delta":"Hel"}`+"\n\n")
		flusher.Flush()
		time.Sleep(20 * time.Millisecond)
		_, _ = io.WriteString(w, `data: {"type":"response.output_text.delta","output_index":0,"delta":"lo"}`+"\n\n")
		_, _ = io.WriteString(w, `data: {"type":"response.output_item.done","output_index":0,"item":{"type":"message","id":"m1","role":"assistant","content":[{"type":"output_text","text":"Hello"}]}}`+"\n\n")
		_, _ = io.WriteString(w, `data: {"type":"response.done","response":{"id":"resp-1","status":"completed","end_turn":true,"output":[],"usage":{"input_tokens":10,"output_tokens":5,"total_tokens":15}}}`+"\n\n")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer server.Close()

	stream := StreamOpenAICodexResponses(codexModel(server.URL), NormalizeContext(Context{
		Messages: []Message{&UserMessage{Content: StringOrBlocks{Text: "hi"}, Timestamp: 1}},
	}), &OpenAICodexResponsesOptions{
		StreamOptions: StreamOptions{APIKey: codexToken(t, "acct-1"), TimeoutMs: intPtrT(2000)},
		Transport:     "sse",
	})
	message, err := stream.Result(context.Background())
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if message.StopReason != StopStop {
		t.Fatalf("stop reason = %v, message = %v", message.StopReason, *message.ErrorMessage)
	}
	if len(message.Content) != 1 {
		t.Fatalf("content = %+v", message.Content)
	}
	if text, ok := message.Content[0].(TextContent); !ok || text.Text != "Hello" {
		t.Fatalf("content = %+v; want the complete stream", message.Content[0])
	}
}

// Image generation is a single request with the same setting: a server that
// never answers must not hang the call (upstream passes timeoutMs to
// api/openrouter-images.ts's request too).
func TestOpenRouterImagesAbortsWhenResponseHeadersIdle(t *testing.T) {
	server := stallServer(t, false, "")
	model := &ImagesModel{
		ID: "flux", Name: "FLUX", API: KnownImagesApiOpenRouter, Provider: "openrouter",
		BaseURL: server.URL, Input: []string{"text"}, Output: []string{"image"},
	}
	result := OpenRouterImages{}.GenerateImages(model, ImagesContext{Input: []ImagesInputContent{
		TextContent{Text: "a cat"},
	}}, &ImagesOptions{StreamOptions: StreamOptions{APIKey: "test", TimeoutMs: intPtrT(200)}})
	if result.StopReason != ImagesStopError {
		t.Fatalf("stop reason = %v", result.StopReason)
	}
	if result.ErrorMessage == nil || !strings.Contains(*result.ErrorMessage, "headers idle") {
		t.Fatalf("error message = %v; want a headers-idle error", result.ErrorMessage)
	}
}

// stepReader hands out bytes one at a time so a test can control when the
// consumer asks for the next one.
type stepReader struct {
	data    []byte
	blocked chan struct{}
}

func (r *stepReader) Read(p []byte) (int, error) {
	if len(r.data) == 0 {
		<-r.blocked
	}
	p[0] = r.data[0]
	r.data = r.data[1:]
	return 1, nil
}

func (r *stepReader) Close() error { return nil }

// The idle timeout measures time blocked waiting for bytes, not wall-clock
// silence while the consumer is busy: a loaded machine, a GC pause or a slow
// renderer must not abort a connection whose bytes already arrived.
func TestIdleGuardDoesNotFireWhileTheConsumerIsBusy(t *testing.T) {
	parent, parentCancel := context.WithCancel(context.Background())
	defer parentCancel()
	cancelled := make(chan struct{})
	var once sync.Once
	body := newIdleBody(parent, func() {
		once.Do(func() { close(cancelled) })
		parentCancel()
	}, 30*time.Millisecond)
	body.gotHeaders.Store(true)
	body.rc = &stepReader{data: []byte("abc"), blocked: make(chan struct{})}
	defer body.Close()

	buf := make([]byte, 1)
	if n, err := body.Read(buf); err != nil || n != 1 || string(buf) != "a" {
		t.Fatalf("first read = %q, %v", buf, err)
	}
	time.Sleep(200 * time.Millisecond) // the consumer is busy for ~6 timeouts
	select {
	case <-cancelled:
		t.Fatal("idle guard cancelled the request while the consumer was not reading")
	default:
	}
	if n, err := body.Read(buf); err != nil || n != 1 || string(buf) != "b" {
		t.Fatalf("second read = %q, %v; the guard broke a live body", buf, err)
	}
}

// Mistral bounds response headers with its own upstream-shaped message and the
// same settings timeout bounds a silent body (D254).
func TestStreamMistralAbortsWhenStreamIdles(t *testing.T) {
	server := stallServer(t, true, `data: {"choices":[{"delta":{"content":"thinking"}}]}`+"\n\n")
	stream := StreamMistralConversations(mistralModel(server.URL), mistralUserContext(), &MistralOptions{
		StreamOptions: StreamOptions{APIKey: "test", TimeoutMs: intPtrT(200)},
	})
	awaitStreamError(t, stream, "stream idle")
}
