package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"
)

// Port of pi v1.5.2 (b4afdddf) test/anthropic-message-threads.test.ts.
// The fake endpoint records request bodies and beta lists and answers from a
// queue or with a plain reply; like the real server it rejects a continue whose
// fixed fields differ from the thread's.

const threadTestGrammarError = "The compiled grammar is too large, which would cause performance issues. " +
	"Simplify your tool schemas or reduce the number of strict tools."

// Upstream test: "creates a new thread when the request has no messages beyond
// the stored prefix".
func TestAnthropicMessageThreadsStartNewThreadWithoutNewMessages(t *testing.T) {
	h := newThreadTestHarness(t)
	messages := []Message{threadTestUser("first")}
	messages, _ = h.say(t, messages, threadTurn{})
	h.turn(t, messages, threadTurn{})
	requireThreadType(t, h.request(t, 1), "create")
}

// Upstream test: "turns threads off for the session after %s on continue".
func TestAnthropicMessageThreadsTurnOffWhenUnsupported(t *testing.T) {
	cases := []struct {
		name      string
		errorCode string
		message   string
		reason    string
	}{
		{"thread_unsupported_request", "thread_unsupported_request", "thread_unsupported_request", "thread_unsupported_request"},
		{"a 400 without error code about the thread field", "", "thread: Extra inputs are not permitted", "400"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			h := newThreadTestHarness(t)
			messages := []Message{threadTestUser("first")}
			messages, _ = h.say(t, messages, threadTurn{})
			messages = append(messages, threadTestUser("second"))
			h.push(func(threadTestCaptured) threadTestReply {
				return threadTestErrorReply(http.StatusBadRequest, testCase.message, testCase.errorCode, nil)
			})
			messages, result := h.say(t, messages, threadTurn{})
			if result.StopReason != StopStop {
				t.Fatalf("stop reason = %q", result.StopReason)
			}
			requireThreadFallbackDiagnostic(t, result, testCase.reason)

			messages = append(messages, threadTestUser("third"))
			h.say(t, messages, threadTurn{})
			later := h.request(t, 3)
			requireThreadType(t, later, "")
			// The resend was accepted with the threads beta, so the beta list
			// stays the same.
			if !slices.Equal(later.Betas, h.request(t, 0).Betas) {
				t.Fatalf("later betas = %v, want %v", later.Betas, h.request(t, 0).Betas)
			}
		})
	}
}

// Upstream test: "shows the error when the resend fails too and keeps threads on".
func TestAnthropicMessageThreadsKeepThreadsOnWhenResendFails(t *testing.T) {
	h := newThreadTestHarness(t)
	messages := []Message{threadTestUser("first")}
	messages, _ = h.say(t, messages, threadTurn{})
	messages = append(messages, threadTestUser("second"))
	h.push(func(threadTestCaptured) threadTestReply {
		return threadTestErrorReply(http.StatusBadRequest, "thread: Extra inputs are not permitted", "", nil)
	})
	h.push(func(threadTestCaptured) threadTestReply {
		return threadTestErrorReply(http.StatusBadRequest, "Real problem", "", nil)
	})
	failed := h.turn(t, messages, threadTurn{})
	if failed.StopReason != StopError {
		t.Fatalf("stop reason = %q", failed.StopReason)
	}
	if failed.ErrorMessage == nil || !strings.Contains(*failed.ErrorMessage, "Real problem") {
		t.Fatalf("error message = %v", failed.ErrorMessage)
	}
	if h.count() != 3 {
		t.Fatalf("requests = %d", h.count())
	}

	h.turn(t, messages, threadTurn{})
	requireThreadType(t, h.request(t, 3), "create")
}

// Upstream test: "passes a %s that is not about the thread on without resending".
func TestAnthropicMessageThreadsPassThroughOtherErrors(t *testing.T) {
	cases := []struct {
		status  int
		message string
	}{
		{http.StatusBadRequest, "prompt is too long: 250000 tokens > 200000 maximum"},
		{http.StatusNotFound, "model: claude-test"},
	}
	for _, testCase := range cases {
		t.Run(fmt.Sprintf("%d", testCase.status), func(t *testing.T) {
			h := newThreadTestHarness(t)
			messages := []Message{threadTestUser("first")}
			messages, _ = h.say(t, messages, threadTurn{})
			messages = append(messages, threadTestUser("second"))
			h.push(func(threadTestCaptured) threadTestReply {
				return threadTestErrorReply(testCase.status, testCase.message, "", nil)
			})
			failed := h.turn(t, messages, threadTurn{})
			if failed.StopReason != StopError {
				t.Fatalf("stop reason = %q", failed.StopReason)
			}
			if failed.ErrorMessage == nil || !strings.Contains(*failed.ErrorMessage, testCase.message) {
				t.Fatalf("error message = %v", failed.ErrorMessage)
			}
			if h.count() != 2 {
				t.Fatalf("requests = %d", h.count())
			}
			if len(failed.Diagnostics) != 0 {
				t.Fatalf("diagnostics = %+v", failed.Diagnostics)
			}

			// Threads stay on: after a fix such as compaction, the next request
			// starts a new thread.
			h.turn(t, messages, threadTurn{})
			requireThreadType(t, h.request(t, 2), "create")
		})
	}
}

// Upstream test: "leaves strict tool rejections to the strict tool fallback".
func TestAnthropicMessageThreadsLeaveStrictToolRejectionsAlone(t *testing.T) {
	model := threadTestModel()
	model.Compat = &ModelCompat{AnthropicMessages: &AnthropicMessagesCompat{SupportsStrictTools: new(true)}}
	strictTool := threadTestTool("lookup")
	strictTool.ConstrainedSampling = ConstrainedSamplingValue{
		Set: true, Config: &ConstrainedSamplingConfig{Type: "json_schema", Strict: "prefer"},
	}
	h := newThreadTestHarness(t)
	h.push(func(threadTestCaptured) threadTestReply {
		return threadTestErrorReply(http.StatusBadRequest, threadTestGrammarError, "", nil)
	})
	result := h.turn(t, []Message{threadTestUser("first")}, threadTurn{Model: model, Tools: []Tool{strictTool}})
	if result.StopReason != StopStop {
		t.Fatalf("stop reason = %q", result.StopReason)
	}
	if h.count() != 2 {
		t.Fatalf("requests = %d", h.count())
	}
	if got, want := threadTypes(h.snapshot()), []string{"create", "create"}; !slices.Equal(got, want) {
		t.Fatalf("thread types = %v, want %v", got, want)
	}
	if len(result.Diagnostics) != 1 || result.Diagnostics[0].Type != "anthropic_strict_tools_fallback" {
		t.Fatalf("diagnostics = %+v", result.Diagnostics)
	}
}

// Upstream test: "fails once, then turns threads off, with an explicit
// anthropic-beta header %s".
func TestAnthropicMessageThreadsExplicitBetaHeader(t *testing.T) {
	cases := []struct {
		name   string
		header *string
	}{
		{"without the threads beta", new("fine-grained-tool-streaming-2025-05-14")},
		{"set to null", nil},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			h := newThreadTestHarness(t)
			respond := func(request threadTestCaptured) threadTestReply {
				if request.thread() != nil && !slices.Contains(request.Betas, threadTestThreadsBeta) {
					return threadTestErrorReply(http.StatusBadRequest, "thread: Extra inputs are not permitted", "", nil)
				}
				return threadTestSSE(request, threadTestReplySpec{ID: request.ID})
			}
			h.push(respond)
			h.push(respond)
			h.push(respond)
			headers := ProviderHeaders{"anthropic-beta": testCase.header}
			messages := []Message{threadTestUser("first")}
			messages, result := h.say(t, messages, threadTurn{Options: func(options *AnthropicOptions) {
				options.Headers = headers
			}})
			if result.StopReason != StopStop {
				t.Fatalf("stop reason = %q", result.StopReason)
			}
			messages = append(messages, threadTestUser("second"))
			h.say(t, messages, threadTurn{Options: func(options *AnthropicOptions) {
				options.Headers = headers
			}})
			if got, want := threadTypes(h.snapshot()), []string{"create", "", ""}; !slices.Equal(got, want) {
				t.Fatalf("thread types = %v, want %v", got, want)
			}
		})
	}
}

// Upstream test: "does not record %s" (an aborted reply, a stream error, a
// stream that ends early, a refusal, a fallback model reply).
func TestAnthropicMessageThreadsDoNotRecordFailedReplies(t *testing.T) {
	cases := []struct {
		name string
		end  string
		spec threadTestReplySpec
	}{
		{"a stream error", "error_event", threadTestReplySpec{ID: "msg_bad"}},
		{"a stream that ends early", "no_message_stop", threadTestReplySpec{ID: "msg_bad"}},
		{"a refusal", "", threadTestReplySpec{ID: "msg_bad", StopReason: "refusal"}},
		{"a fallback model reply", "", threadTestReplySpec{ID: "msg_bad", Model: "claude-fallback"}},
	}
	for _, testCase := range cases {
		t.Run("does not record "+testCase.name, func(t *testing.T) {
			h := newThreadTestHarness(t)
			spec := testCase.spec
			spec.End = testCase.end
			h.push(func(request threadTestCaptured) threadTestReply {
				return threadTestSSE(request, spec)
			})
			messages := []Message{threadTestUser("first")}
			first := h.turn(t, messages, threadTurn{})
			messages = append(messages, first, threadTestUser("second"))
			h.turn(t, messages, threadTurn{})
			requireThreadType(t, h.request(t, 1), "create")
		})
	}
	t.Run("does not record an aborted reply", func(t *testing.T) {
		h := newThreadTestHarness(t)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		messages := []Message{threadTestUser("first")}
		first := h.turn(t, messages, threadTurn{Options: func(options *AnthropicOptions) {
			options.Ctx = ctx
			options.OnProviderStreamEvent = func(data json.RawMessage, model *Model) {
				var event struct {
					Type string `json:"type"`
				}
				if json.Unmarshal(data, &event) == nil && event.Type == "message_start" {
					cancel()
				}
			}
		}})
		messages = append(messages, first, threadTestUser("second"))
		h.turn(t, messages, threadTurn{})
		requireThreadType(t, h.request(t, 1), "create")
	})
}

// Upstream test: "does not use threads with cacheRetention none".
func TestAnthropicMessageThreadsOffWithCacheRetentionNone(t *testing.T) {
	h := newThreadTestHarness(t)
	messages := []Message{threadTestUser("first")}
	h.say(t, messages, threadTurn{Options: func(options *AnthropicOptions) {
		options.CacheRetention = CacheRetentionNone
	}})
	request := h.request(t, 0)
	requireThreadType(t, request, "")
	if slices.Contains(request.Betas, threadTestThreadsBeta) {
		t.Fatalf("betas = %v", request.Betas)
	}
}

// Upstream test: "matches Claude Code tool names across turns with OAuth".
func TestAnthropicMessageThreadsMatchClaudeCodeToolNamesAcrossTurns(t *testing.T) {
	h := newThreadTestHarness(t)
	h.push(func(request threadTestCaptured) threadTestReply {
		return threadTestSSE(request, threadTestReplySpec{
			ID: "msg_oauth", ToolCalls: []threadTestToolCall{{ID: "toolu_1", Name: "Read", Input: map[string]any{"value": "x"}}},
		})
	})
	messages := []Message{threadTestUser("read it")}
	messages, first := h.say(t, messages, threadTurn{Tools: []Tool{threadTestTool("read")}, Options: func(options *AnthropicOptions) {
		options.APIKey = "sk-ant-oat-test"
	}})
	found := false
	for _, block := range first.Content {
		if call, ok := block.(ToolCall); ok && call.Name == "read" {
			found = true
		}
	}
	if !found {
		t.Fatalf("content = %+v", first.Content)
	}
	messages = append(messages, threadTestToolResult("toolu_1", "read"))
	h.say(t, messages, threadTurn{Tools: []Tool{threadTestTool("read")}, Options: func(options *AnthropicOptions) {
		options.APIKey = "sk-ant-oat-test"
	}})
	requireThreadJSON(t, h.request(t, 1).thread(), `{"type":"continue","previous_message_id":"msg_oauth"}`)
}

// Upstream test: "sends cache warming replays without a thread and keeps the
// conversation's thread".
func TestAnthropicMessageThreadsSkipDisabledContinuation(t *testing.T) {
	h := newThreadTestHarness(t)
	messages := []Message{threadTestUser("first")}
	messages, _ = h.say(t, messages, threadTurn{})
	h.turn(t, messages[:1], threadTurn{Options: func(options *AnthropicOptions) {
		options.MaxTokens = new(1)
		options.DisableContinuation = true
	}})
	messages = append(messages, threadTestUser("second"))
	h.say(t, messages, threadTurn{})

	real, warm, next := h.request(t, 0), h.request(t, 1), h.request(t, 2)
	requireThreadType(t, warm, "")
	if warm.Body["max_tokens"] != float64(1) {
		t.Fatalf("max_tokens = %v", warm.Body["max_tokens"])
	}
	requireThreadJSON(t, warm.messages(), string(mustMarshalJSON(real.messages())))
	if !slices.Equal(warm.Betas, real.Betas) {
		t.Fatalf("warm betas = %v, want %v", warm.Betas, real.Betas)
	}
	requireThreadJSON(t, next.thread(), `{"type":"continue","previous_message_id":"msg_1"}`)
}

// Reference: pi v1.5.2 (b4afdddf) api/anthropic-messages.ts getAnthropicCompat:
// message threads default on only for provider anthropic on api.anthropic.com,
// and a compat entry overrides the default either way.
func TestAnthropicMessageThreadsCompatDefault(t *testing.T) {
	cases := []struct {
		name     string
		provider ProviderId
		baseURL  string
		compat   *bool
		want     bool
	}{
		{"anthropic endpoint", "anthropic", "https://api.anthropic.com", nil, true},
		{"anthropic proxy", "anthropic", "https://proxy.example.com", nil, false},
		{"other provider on the anthropic endpoint", "openrouter", "https://api.anthropic.com", nil, false},
		{"compat enables a proxy", "anthropic", "https://proxy.example.com", new(true), true},
		{"compat disables the anthropic endpoint", "anthropic", "https://api.anthropic.com", new(false), false},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			model := threadTestModel()
			model.Provider = testCase.provider
			model.BaseURL = testCase.baseURL
			if testCase.compat != nil {
				model.Compat = &ModelCompat{AnthropicMessages: &AnthropicMessagesCompat{SupportsMessageThreads: testCase.compat}}
			}
			if got := GetAnthropicCompat(model).SupportsMessageThreads; got != testCase.want {
				t.Fatalf("supportsMessageThreads = %v, want %v", got, testCase.want)
			}
			if got := slices.Contains(GetBetaFeatures(model, TranscriptContext{}, false, false, testCase.want, nil), MessageThreadsBeta); got != testCase.want {
				t.Fatalf("threads beta = %v, want %v", got, testCase.want)
			}
		})
	}
}

// A model on a non-Anthropic endpoint sends no thread and no threads beta,
// unless its models.json compat entry asks for threads.
func TestAnthropicMessageThreadsFollowCompatOnProxyEndpoints(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		compat *bool
		want   string
	}{
		{"default off", nil, ""},
		{"compat on", new(true), "create"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			h := newThreadTestHarness(t)
			model := threadTestModel()
			model.BaseURL = "https://proxy.example.com"
			if testCase.compat != nil {
				model.Compat = &ModelCompat{AnthropicMessages: &AnthropicMessagesCompat{SupportsMessageThreads: testCase.compat}}
			}
			messages := []Message{threadTestUser("first")}
			messages, _ = h.say(t, messages, threadTurn{Model: model})
			messages = append(messages, threadTestUser("second"))
			h.say(t, messages, threadTurn{Model: model})
			requireThreadType(t, h.request(t, 0), testCase.want)
			if testCase.want != "" {
				requireThreadType(t, h.request(t, 1), "continue")
			} else {
				requireThreadType(t, h.request(t, 1), "")
			}
			hasBeta := slices.Contains(h.request(t, 0).Betas, threadTestThreadsBeta)
			if hasBeta != (testCase.want != "") {
				t.Fatalf("betas = %v", h.request(t, 0).Betas)
			}
		})
	}
}

// The thread map is bounded: the least recently updated session is dropped.
func TestAnthropicMessageThreadsStateIsBounded(t *testing.T) {
	ResetAnthropicMessageThreads("")
	t.Cleanup(func() { ResetAnthropicMessageThreads("") })
	for index := 0; index < maxAnthropicThreadStateEntries+2; index++ {
		setAnthropicThreadState(fmt.Sprintf("thread-bound-%d", index), &anthropicThreadEntry{prefixCount: index})
	}
	anthropicThreadMu.Lock()
	size := len(anthropicThreadEntries)
	ordered := len(anthropicThreadOrder)
	anthropicThreadMu.Unlock()
	if size != maxAnthropicThreadStateEntries || ordered != maxAnthropicThreadStateEntries {
		t.Fatalf("entries = %d/%d, want %d", size, ordered, maxAnthropicThreadStateEntries)
	}
	if _, known := getAnthropicThreadState("thread-bound-0"); known {
		t.Fatal("oldest session kept")
	}
	if _, known := getAnthropicThreadState("thread-bound-1"); known {
		t.Fatal("second oldest session kept")
	}
	if _, known := getAnthropicThreadState("thread-bound-2"); !known {
		t.Fatal("newest session dropped")
	}
	ResetAnthropicMessageThreads("thread-bound-2")
	if _, known := getAnthropicThreadState("thread-bound-2"); known {
		t.Fatal("reset kept the session")
	}
}

// Reference: pi v1.5.2 (b4afdddf) api/anthropic-messages.ts hashThreadPrefix.
// Its replacer drops cache_control and sends string content as one text block;
// the port keeps that normalization. D459: the hash itself is canonical JSON
// with sorted keys, so member order does not change it.
func TestAnthropicMessageThreadsPrefixHashNormalization(t *testing.T) {
	model := threadTestModel()
	settings, err := decodeOrderedJSONFields(json.RawMessage(`{"model":"claude-test"}`))
	if err != nil {
		t.Fatal(err)
	}
	hash := func(raw ...string) string {
		t.Helper()
		messages := make([]json.RawMessage, 0, len(raw))
		for _, message := range raw {
			messages = append(messages, json.RawMessage(message))
		}
		return hashAnthropicThreadPrefix(model, settings, messages)
	}
	cached := hash(`{"role":"user","content":[{"type":"text","text":"hi","cache_control":{"type":"ephemeral"}}]}`)
	plain := hash(`{"role":"user","content":[{"type":"text","text":"hi"}]}`)
	if cached != plain {
		t.Fatalf("cache marker changed the hash: %q vs %q", cached, plain)
	}
	if asString := hash(`{"role":"user","content":"hi"}`); asString != plain {
		t.Fatalf("string content changed the hash: %q vs %q", asString, plain)
	}
	if reordered := hash(`{"content":"hi","role":"user"}`); reordered != plain {
		t.Fatalf("member order changed the hash: %q vs %q", reordered, plain)
	}
	if changed := hash(`{"role":"user","content":"bye"}`); changed == plain {
		t.Fatalf("different content hashed alike: %q", changed)
	}
	otherSettings, err := decodeOrderedJSONFields(json.RawMessage(`{"model":"claude-other"}`))
	if err != nil {
		t.Fatal(err)
	}
	if hashAnthropicThreadPrefix(model, otherSettings, []json.RawMessage{json.RawMessage(`{"role":"user","content":"hi"}`)}) == plain {
		t.Fatal("a changed setting hashed alike")
	}
}
