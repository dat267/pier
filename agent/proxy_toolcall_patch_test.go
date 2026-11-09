package agent

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dat267/pier/ai"
)

// pi v1.5.0 (f9a26365) packages/agent/src/proxy.ts toolcall_end merges the
// payload into the accumulated call with `Object.assign(content, proxyEvent.toolCall)`,
// so a field the server omitted keeps the value that toolcall_start and the
// deltas produced. Pier replaced the whole struct, so a partial payload erased
// the tool's id and name and the call could no longer be dispatched.
func TestProxyToolcallEndMergesPartialPayload(t *testing.T) {
	partial := &ai.AssistantMessage{Content: ai.ContentList{}}
	scratch := newProxyEventScratch()
	if _, err := processProxyEvent(&proxyWireEvent{Type: "toolcall_start", ContentIndex: 0, ID: "c1", ToolName: "read"}, partial, scratch); err != nil {
		t.Fatal(err)
	}
	if _, err := processProxyEvent(&proxyWireEvent{Type: "toolcall_delta", ContentIndex: 0, Delta: `{"path":"a"}`}, partial, scratch); err != nil {
		t.Fatal(err)
	}
	arguments := json.RawMessage(`{"path":"b"}`)
	event, err := processProxyEvent(&proxyWireEvent{
		Type:         "toolcall_end",
		ContentIndex: 0,
		ToolCall:     &proxyToolCallPatch{Arguments: &arguments},
	}, partial, scratch)
	if err != nil {
		t.Fatal(err)
	}
	if event == nil || event.ToolCall == nil {
		t.Fatalf("toolcall_end event = %+v", event)
	}
	if event.ToolCall.ID != "c1" || event.ToolCall.Name != "read" {
		t.Errorf("tool call = %+v, want the accumulated id and name", *event.ToolCall)
	}
	if string(event.ToolCall.Arguments) != `{"path":"b"}` {
		t.Errorf("arguments = %s, want the payload's", event.ToolCall.Arguments)
	}
}

// An explicit field in the payload replaces the accumulated one.
func TestProxyToolcallEndPayloadOverridesFields(t *testing.T) {
	partial := &ai.AssistantMessage{Content: ai.ContentList{}}
	scratch := newProxyEventScratch()
	if _, err := processProxyEvent(&proxyWireEvent{Type: "toolcall_start", ContentIndex: 0, ID: "c1", ToolName: "read"}, partial, scratch); err != nil {
		t.Fatal(err)
	}
	name := "write"
	signature := "sig"
	event, err := processProxyEvent(&proxyWireEvent{
		Type:         "toolcall_end",
		ContentIndex: 0,
		ToolCall:     &proxyToolCallPatch{Name: &name, ThoughtSignature: &signature},
	}, partial, scratch)
	if err != nil {
		t.Fatal(err)
	}
	if event == nil || event.ToolCall == nil {
		t.Fatalf("toolcall_end event = %+v", event)
	}
	if event.ToolCall.ID != "c1" || event.ToolCall.Name != "write" {
		t.Errorf("tool call = %+v", *event.ToolCall)
	}
	if event.ToolCall.ThoughtSignature == nil || *event.ToolCall.ThoughtSignature != "sig" {
		t.Errorf("thought signature = %v, want sig", event.ToolCall.ThoughtSignature)
	}
	if string(event.ToolCall.Arguments) != `{}` {
		t.Errorf("arguments = %s, want the accumulated object", event.ToolCall.Arguments)
	}
}

// The wire event carries the same partial payload: a server that sends only the
// finished arguments must not erase the tool's id and name on the wire path.
func TestStreamProxyMergesPartialToolcallEnd(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher, _ := w.(http.Flusher)
		for _, event := range []string{
			`{"type":"toolcall_start","contentIndex":0,"id":"c1","toolName":"read"}`,
			`{"type":"toolcall_delta","contentIndex":0,"delta":"{\"path\":\"a\"}"}`,
			`{"type":"toolcall_end","contentIndex":0,"toolCall":{"arguments":{"path":"a"}}}`,
			`{"type":"done","reason":"toolUse","usage":{"input":1,"output":1,"cacheRead":0,"cacheWrite":0,"totalTokens":2,"cost":{"input":0,"output":0,"cacheRead":0,"cacheWrite":0,"total":0}}}`,
		} {
			_, _ = io.WriteString(w, "data: "+event+"\n\n")
			if flusher != nil {
				flusher.Flush()
			}
		}
	}))
	defer server.Close()

	model := &ai.Model{ID: "m", Provider: "p", API: ai.APIOpenAIResponses}
	stream := StreamProxy(model, proxyContext(), ProxyStreamOptions{ProxyURL: server.URL, AuthToken: "tok"})
	result, err := stream.Result(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Content) != 1 {
		t.Fatalf("content = %+v", result.Content)
	}
	call, ok := result.Content[0].(ai.ToolCall)
	if !ok || call.ID != "c1" || call.Name != "read" || string(call.Arguments) != `{"path":"a"}` {
		t.Fatalf("toolcall = %+v", result.Content[0])
	}
}
