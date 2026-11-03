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

func referenceContentBlock(t *testing.T, c Content) []byte {
	t.Helper()
	enc, err := marshalJSONGeneral(c)
	if err != nil {
		t.Fatal(err)
	}
	kindEnc, err := marshalJSONGeneral(c.(interface{ contentKind() ContentKind }).contentKind())
	if err != nil {
		t.Fatal(err)
	}
	out := []byte(`{"type":`)
	out = append(out, kindEnc...)
	out = append(out, ',')
	out = append(out, enc[1:]...)
	return out
}

func TestNilContentBlockEncodesAsNull(t *testing.T) {
	var text *TextContent
	got, err := MarshalJSON(ContentList{text})
	if err != nil || string(got) != "[null]" {
		t.Fatalf("nil block = %q/%v, want [null]", got, err)
	}
}

func TestContentBlockEncodingMatchesGeneralEncoder(t *testing.T) {
	empty := ""
	sig := "sig\u2028<&>"
	ns := "ns"
	cases := []Content{
		TextContent{},
		TextContent{Text: "a\u0001\"\\<&>\u2028", TextSignature: &empty},
		&TextContent{Text: "ptr", TextSignature: &sig},
		ThinkingContent{Thinking: "t", ThinkingSignature: &sig, Redacted: true},
		&ThinkingContent{Thinking: "t2"},
		ImageContent{Data: "aa", MimeType: "image/png"},
		ToolCall{ID: "id", Name: "n", Arguments: json.RawMessage(`{"b":1,"a":2}`)},
		ToolCall{ID: "id", Name: "n", Arguments: json.RawMessage(`[1]`), ThoughtSignature: &sig, Namespace: &ns, ArgumentsError: "bad"},
		&ToolCall{ID: "id", Name: "n", Arguments: json.RawMessage(`null`)},
	}
	for _, c := range cases {
		got, err := marshalContentBlock(c)
		if err != nil {
			t.Fatal(err)
		}
		want := referenceContentBlock(t, c)
		if string(got) != string(want) {
			t.Fatalf("%T = %s, want %s", c, got, want)
		}
	}
}
