package ai

import (
	"encoding/json"
	"os"
	"testing"
)

// Reference: pi v1.5.2 (b4afdddf), packages/ai/src/utils/streaming-json.ts
// ToolCallArgumentsStream. The corpus in testdata/streaming-json is produced by
// running the reference over each text one code point at a time, so this test
// compares the port's incremental parser against the reference rather than
// against expectations written by hand: every preview after every prefix, then
// the final arguments. Only whether a malformed text failed is compared; D221
// records that its message is the Go decoder's.
func TestToolCallArgumentsStreamMatchesReference(t *testing.T) {
	data, err := os.ReadFile("testdata/streaming-json/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Text     string   `json:"text"`
		Previews []string `json:"previews"`
		Final    string   `json:"final"`
		HasError bool     `json:"hasError"`
	}
	if err := json.Unmarshal(data, &fixtures); err != nil {
		t.Fatal(err)
	}
	if len(fixtures) == 0 {
		t.Fatal("the streaming-json corpus is empty")
	}
	for _, fixture := range fixtures {
		var args streamingArguments
		previews := fixture.Previews
		if got := string(args.Append("")); got != previews[0] {
			t.Fatalf("%q: empty preview %q, want %q", fixture.Text, got, previews[0])
		}
		for cut, character := range []rune(fixture.Text) {
			want := previews[cut+1]
			if got := string(args.Append(string(character))); got != want {
				t.Fatalf("%q: preview after %d code points is %q, want %q", fixture.Text, cut+1, got, want)
			}
		}
		final, message := args.Finish()
		if string(final) != fixture.Final {
			t.Fatalf("%q: final arguments %q, want %q", fixture.Text, final, fixture.Final)
		}
		if (message != "") != fixture.HasError {
			t.Fatalf("%q: error %q, want hasError %t", fixture.Text, message, fixture.HasError)
		}
	}
}
