package mcp

import (
	"strings"
	"testing"
)

// pi v1.4.0 streamable-http.ts dispatch omits falsey event/id metadata, but
// processLine still reports an empty id to onId so resumption can be reset.
func TestConsumeSseStreamOmitsEmptyEventMetadata(t *testing.T) {
	var events []SseEvent
	var ids []string
	err := ConsumeSseStream(strings.NewReader("event:\nid:\ndata: hello\n\n"), ConsumeSseOptions{
		OnEvent: func(event SseEvent) { events = append(events, event) },
		OnID:    func(id string) { ids = append(ids, id) },
	})
	if err != nil || len(events) != 1 || events[0].Event != nil || events[0].ID != nil || events[0].Data != "hello" {
		t.Fatalf("events=%+v error=%v, want no event/id metadata", events, err)
	}
	if len(ids) != 1 || ids[0] != "" {
		t.Fatalf("onId values=%q, want an empty id reset", ids)
	}
}
