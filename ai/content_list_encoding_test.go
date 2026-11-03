package ai

import (
	"encoding/json"
	"testing"
)

func TestContentListEncodingMatchesRawRoundTrip(t *testing.T) {
	lists := []ContentList{
		nil,
		{TextContent{Text: "a\u2028<&>"}, ThinkingContent{Thinking: "t"}, ToolCall{ID: "c", Name: "bash", Arguments: json.RawMessage(`{"a":1}`)}},
	}
	for _, list := range lists {
		raw := make([]json.RawMessage, 0, len(list))
		for _, c := range list {
			enc, err := marshalContentBlock(c)
			if err != nil {
				t.Fatal(err)
			}
			raw = append(raw, enc)
		}
		want, err := MarshalJSON(raw)
		if err != nil {
			t.Fatal(err)
		}
		got, err := MarshalJSON(list)
		if err != nil || string(got) != string(want) {
			t.Fatalf("%v = %q/%v, want %q", list, got, err, want)
		}
	}
}
