package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Port of the message-threads half of pi v1.5.2 (b4afdddf)
// api/anthropic-messages.ts: MESSAGE_THREADS_BETA through recordThreadReply.
//
// A thread is server-side conversation state: the server keeps the messages of
// a thread, so a later request of the same session sends only the new messages
// and continues from the previous reply. pi still builds the full request;
// prepareAnthropicThreadRequest compares it with the previous request of the
// session and sends only the new part when nothing earlier changed.

// AnthropicThreadParam is the `thread` request field.
type AnthropicThreadParam struct {
	Type string `json:"type"` // "create" | "continue"
	// PreviousMessageID is the reply a continue builds on.
	PreviousMessageID string `json:"previous_message_id,omitempty"`
}

// anthropicThreadEntry is where a session's next request can continue.
type anthropicThreadEntry struct {
	// prefixCount is the number of messages in the last full request as sent
	// plus the converted reply.
	prefixCount int
	// prefixHash is hashAnthropicThreadPrefix of the request's settings and
	// those messages.
	prefixHash string
	// replyID is the id of the reply to continue from.
	replyID string
}

// Per session: where the next request can continue, or a nil entry after the
// endpoint rejected threads.
var (
	anthropicThreadMu      sync.Mutex
	anthropicThreadEntries = map[string]*anthropicThreadEntry{}
	// anthropicThreadOrder keeps the entries oldest-updated first, so the cap
	// drops the least recently updated session.
	anthropicThreadOrder []string
)

// maxAnthropicThreadStateEntries bounds the memory of sessions that are never
// cleaned up, for example a server passing a new session id per conversation.
// Dropping the least recently updated session is cheap: its thread has most
// likely expired on the server already, and otherwise its next request just
// starts a new thread.
const maxAnthropicThreadStateEntries = 10_000

// setAnthropicThreadState records a session's thread state, keeping at most
// maxAnthropicThreadStateEntries sessions. State is normally freed by
// CleanupSessionResources when a session ends, as pi's coding agent does.
func setAnthropicThreadState(sessionID string, entry *anthropicThreadEntry) {
	anthropicThreadMu.Lock()
	defer anthropicThreadMu.Unlock()
	removeAnthropicThreadLocked(sessionID)
	anthropicThreadEntries[sessionID] = entry
	anthropicThreadOrder = append(anthropicThreadOrder, sessionID)
	for len(anthropicThreadOrder) > maxAnthropicThreadStateEntries {
		delete(anthropicThreadEntries, anthropicThreadOrder[0])
		anthropicThreadOrder = anthropicThreadOrder[1:]
	}
}

// removeAnthropicThreadState forgets a session's thread state.
func removeAnthropicThreadState(sessionID string) {
	anthropicThreadMu.Lock()
	defer anthropicThreadMu.Unlock()
	removeAnthropicThreadLocked(sessionID)
}

func removeAnthropicThreadLocked(sessionID string) {
	if _, ok := anthropicThreadEntries[sessionID]; !ok {
		return
	}
	delete(anthropicThreadEntries, sessionID)
	for index, candidate := range anthropicThreadOrder {
		if candidate == sessionID {
			anthropicThreadOrder = append(anthropicThreadOrder[:index], anthropicThreadOrder[index+1:]...)
			return
		}
	}
}

// getAnthropicThreadState returns a session's entry; a nil entry with known
// true means the endpoint rejected threads ("off").
func getAnthropicThreadState(sessionID string) (entry *anthropicThreadEntry, known bool) {
	anthropicThreadMu.Lock()
	defer anthropicThreadMu.Unlock()
	entry, known = anthropicThreadEntries[sessionID]
	return entry, known
}

// ResetAnthropicMessageThreads forgets one session's thread state, or every
// session's when the id is empty. CleanupSessionResources calls it when a
// session ends (upstream registers the same cleanup in session-resources.ts).
func ResetAnthropicMessageThreads(sessionID string) {
	anthropicThreadMu.Lock()
	defer anthropicThreadMu.Unlock()
	if sessionID == "" {
		anthropicThreadEntries = map[string]*anthropicThreadEntry{}
		anthropicThreadOrder = nil
		return
	}
	removeAnthropicThreadLocked(sessionID)
}

// newThreadErrors are rejections of a continue that a new thread fixes:
// expired, raced, or a fixed setting changed.
var newThreadErrors = map[string]bool{
	"thread_not_found":            true,
	"thread_already_continued":    true,
	"thread_continue_in_flight":   true,
	"thread_fingerprint_mismatch": true,
}

// hashAnthropicThreadPrefix hashes everything a continue builds on: the
// settings that must stay the same within a thread and the messages the server
// already holds.
//
// Settings, as sent: the endpoint (a thread exists on one endpoint only) and
// the fixed request fields. A continue omits system and tools and the server
// keeps the thread's own, so only this check can notice a change to them,
// including their cache markers. The server rejects changes to the other fields
// with thread_fingerprint_mismatch; checking them here saves that rejected
// request. A fixed field missing here costs only that rejection, which also
// starts a new thread.
//
// Messages are normalized so messages that mean the same to the API hash the
// same. The hash never leaves the process, so the port hashes a canonical JSON
// form of the same values rather than the reference's byte-exact stringify
// (D459: member order does not change the hash).
func hashAnthropicThreadPrefix(model *Model, settings *orderedJSONFields, messages []json.RawMessage) string {
	values := make([]any, 0, 10)
	values = append(values, string(model.Provider), model.BaseURL)
	for _, key := range []string{
		"model", "system", "tools", "thinking", "output_config", "tool_choice", "betas", "fallbacks",
	} {
		raw, ok := settings.raw(key)
		if !ok {
			values = append(values, nil)
			continue
		}
		values = append(values, decodeThreadJSONValue(raw))
	}
	encodedSettings, err := json.Marshal(values)
	if err != nil {
		return ""
	}
	return ShortHash(string(encodedSettings) + normalizeAnthropicThreadMessages(messages))
}

// decodeThreadJSONValue parses one request field for hashing. Parsed numbers
// keep their spelling so two requests that sent the same bytes hash the same.
func decodeThreadJSONValue(raw json.RawMessage) any {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return string(raw)
	}
	return value
}

// normalizeAnthropicThreadMessages applies upstream's JSON.stringify replacer
// to the messages the server holds: cache markers don't change what a message
// means, but they move (pi puts one on the newest message), and the newest user
// message's string content is sent as one text block to carry that marker.
func normalizeAnthropicThreadMessages(messages []json.RawMessage) string {
	list := make([]any, 0, len(messages))
	for _, raw := range messages {
		list = append(list, normalizeAnthropicThreadValue(decodeThreadJSONValue(raw), ""))
	}
	encoded, err := json.Marshal(list)
	if err != nil {
		return ""
	}
	return string(encoded)
}

// normalizeAnthropicThreadValue drops cache_control members and sends string
// content as one text block, like upstream's replacer. Array elements carry no
// key, so only an object's own "content" member is rewritten.
func normalizeAnthropicThreadValue(value any, key string) any {
	switch v := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(v))
		for name, item := range v {
			if name == "cache_control" {
				continue
			}
			out[name] = normalizeAnthropicThreadValue(item, name)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for index, item := range v {
			out[index] = normalizeAnthropicThreadValue(item, "")
		}
		return out
	case string:
		if key == "content" {
			// "hi" and [{type:"text",text:"hi"}] mean the same message.
			return []any{map[string]any{"type": "text", "text": v}}
		}
		return v
	default:
		return value
	}
}

// anthropicPreparedThread is one prepared thread request: the bytes to send,
// plus the full request they were derived from.
type anthropicPreparedThread struct {
	sessionID string
	// full is the complete request body, before the thread field was added.
	full json.RawMessage
	// settings is full decoded, for the prefix hash.
	settings *orderedJSONFields
	// messages are full's messages, in request order.
	messages []json.RawMessage
	// body is what is sent: full with a new thread, or the continue subset.
	body json.RawMessage
}

// anthropicThreadBody derives a request body from the full request bytes,
// applying mutate to a fresh copy so the full request stays intact for the
// fallback resend.
func anthropicThreadBody(full json.RawMessage, mutate func(*orderedJSONFields)) (json.RawMessage, error) {
	fields, err := decodeOrderedJSONFields(full)
	if err != nil {
		return nil, err
	}
	mutate(fields)
	return fields.marshal()
}

// prepareAnthropicThreadRequest picks the thread request for a session (port
// of prepareThreadRequest). A nil result means the session sends no thread: the
// endpoint rejected threads before.
func prepareAnthropicThreadRequest(model *Model, sessionID string, full json.RawMessage) (*anthropicPreparedThread, error) {
	state, known := getAnthropicThreadState(sessionID)
	if known && state == nil {
		return nil, nil
	}
	settings, err := decodeOrderedJSONFields(full)
	if err != nil {
		return nil, err
	}
	var messages []json.RawMessage
	if rawMessages, ok := settings.raw("messages"); ok {
		if err := jsonUnmarshalStrict(rawMessages, &messages); err != nil {
			return nil, err
		}
	}
	thread := &anthropicPreparedThread{sessionID: sessionID, full: full, settings: settings, messages: messages}
	if known && len(messages) > state.prefixCount &&
		hashAnthropicThreadPrefix(model, settings, messages[:state.prefixCount]) == state.prefixHash {
		// Each reply can be continued once; deleting also keeps a concurrent
		// request from trying.
		removeAnthropicThreadState(sessionID)
		body, err := anthropicThreadBody(full, func(fields *orderedJSONFields) {
			fields.remove("system")
			fields.remove("tools")
			fields.set("messages", mustMarshalJSON(messages[state.prefixCount:]))
			fields.set("thread", mustMarshalJSON(AnthropicThreadParam{
				Type: "continue", PreviousMessageID: state.replyID,
			}))
		})
		if err != nil {
			return nil, err
		}
		thread.body = body
		return thread, nil
	}
	thread.body, err = anthropicThreadBody(full, func(fields *orderedJSONFields) {
		fields.set("thread", mustMarshalJSON(AnthropicThreadParam{Type: "create"}))
	})
	if err != nil {
		return nil, err
	}
	return thread, nil
}

// getAnthropicThreadErrorCode returns the error_code Anthropic puts in
// error.details of thread rejections.
func getAnthropicThreadErrorCode(providerErr *ProviderError) string {
	var envelope struct {
		Error struct {
			Details struct {
				ErrorCode json.RawMessage `json:"error_code"`
			} `json:"details"`
		} `json:"error"`
	}
	if jsonUnmarshalStrict([]byte(providerErr.Body), &envelope) != nil {
		return ""
	}
	var code string
	if jsonUnmarshalStrict(envelope.Error.Details.ErrorCode, &code) != nil {
		return ""
	}
	return code
}

// sendAnthropicThreadRequest sends a thread request. A rejection about the
// thread (never a started stream) is resent once in full: as a new thread when
// the old one can't be continued, or without `thread` when the endpoint can't
// use threads, which also turns threads off for the session. Other errors are
// passed on. The returned thread request is the one that produced the response,
// if any.
func sendAnthropicThreadRequest(
	model *Model,
	thread *anthropicPreparedThread,
	send func(json.RawMessage) (*http.Response, error),
	output *AssistantMessage,
	ctx context.Context,
) (*http.Response, *anthropicPreparedThread, error) {
	response, err := send(thread.body)
	if err == nil {
		return response, thread, nil
	}
	// An aborted request is never resent.
	if ctxErr(ctx) != nil {
		return nil, nil, err
	}
	var providerErr *ProviderError
	if !errors.As(err, &providerErr) {
		return nil, nil, err
	}
	errorCode := getAnthropicThreadErrorCode(providerErr)
	startNewThread := newThreadErrors[errorCode]
	// The endpoint can't use threads: it can't thread this request, or it
	// doesn't know the `thread` field and rejects it without a code ("thread:
	// Extra inputs are not permitted"), e.g. with an anthropic-beta header that
	// lacks the threads beta.
	threadsUnsupported := errorCode == "thread_unsupported_request" ||
		(errorCode == "" && providerErr.Status == http.StatusBadRequest && strings.Contains(providerErr.Message, "thread"))
	// Any other error (prompt too long, unknown model, strict tool limits, ...)
	// is not about the thread, and resending would only fail again.
	if !startNewThread && !threadsUnsupported {
		return nil, nil, err
	}
	// Recorded before the resend, so a failed resend keeps it.
	reason := errorCode
	if reason == "" {
		reason = fmt.Sprintf("%d", providerErr.Status)
	}
	appendAnthropicThreadFallback(output, err, reason)
	if startNewThread {
		// The full request as a new thread; the resend reuses it without the
		// thread field when the endpoint can't use threads.
		body, bodyErr := anthropicThreadBody(thread.full, func(fields *orderedJSONFields) {
			fields.set("thread", mustMarshalJSON(AnthropicThreadParam{Type: "create"}))
		})
		if bodyErr != nil {
			return nil, nil, bodyErr
		}
		created := &anthropicPreparedThread{
			sessionID: thread.sessionID, full: thread.full,
			settings: thread.settings, messages: thread.messages, body: body,
		}
		response, err = send(created.body)
		return response, created, err
	}
	// With the threads beta, a request without `thread` is a normal request.
	response, err = send(thread.full)
	if err != nil {
		return nil, nil, err
	}
	setAnthropicThreadState(thread.sessionID, nil)
	return response, nil, nil
}

// appendAnthropicThreadFallback records a thread rejection that was resent.
func appendAnthropicThreadFallback(output *AssistantMessage, err error, reason string) {
	details, marshalErr := MarshalJSON(struct {
		Reason string `json:"reason"`
	}{reason})
	if marshalErr != nil {
		details = json.RawMessage(`{}`)
	}
	AppendAssistantMessageDiagnostic(output, AssistantMessageDiagnostic{
		Type: "anthropic_thread_fallback", Timestamp: time.Now().UnixMilli(),
		Error:   &DiagnosticErrorInfo{Message: err.Error()},
		Details: details,
	})
}

// recordAnthropicThreadReply records where the session's next request can
// continue: after output, the clean reply to the thread request thread, whose
// full request was the one thread carries.
func recordAnthropicThreadReply(model *Model, thread *anthropicPreparedThread, output *AssistantMessage, isOAuth, allowEmptySignature bool) {
	// responseModel is set only when a fallback model answered; the server does
	// not store those replies, so they cannot be continued.
	if output.ResponseID == nil || output.ResponseModel != nil {
		return
	}
	// Convert the reply like the next request will, keeping only the assistant
	// message (TransformMessages adds placeholder results for trailing tool
	// calls).
	converted, err := ConvertAnthropicMessages(
		TransformMessages([]Message{output}, model, func(id string, model *Model, source *AssistantMessage) string {
			return NormalizeAnthropicToolCallID(id)
		}),
		isOAuth, nil, allowEmptySignature, "", nil,
	)
	if err != nil {
		return
	}
	prefix := make([]json.RawMessage, 0, len(thread.messages)+len(converted.Messages))
	prefix = append(prefix, thread.messages...)
	for _, message := range converted.Messages {
		if message.Role != "assistant" {
			continue
		}
		prefix = append(prefix, mustMarshalJSON(message))
	}
	setAnthropicThreadState(thread.sessionID, &anthropicThreadEntry{
		prefixCount: len(prefix),
		prefixHash:  hashAnthropicThreadPrefix(model, thread.settings, prefix),
		replyID:     *output.ResponseID,
	})
}
