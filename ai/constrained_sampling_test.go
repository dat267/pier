package ai

import "testing"

// buffer.Input is a byte prefix of nextInput, so the delta starts at its
// length. Upstream slices by buffer.input.length (UTF-16), and a Go string
// sliced at a UTF-16 offset would cut multi-byte text; the byte prefix makes
// len(buffer.Input) the exact offset.
func TestAppendGrammarToolInputJSONDeltaSlicesBytes(t *testing.T) {
	buffer := &GrammarToolInputJSONBuffer{}
	delta, ok, err := AppendGrammarToolInputJSONDelta(buffer, "payload", "日本", false)
	if !ok || err != nil {
		t.Fatalf("first delta: ok=%v err=%v", ok, err)
	}
	if delta != `{"payload":"日本` {
		t.Fatalf("first delta = %q", delta)
	}
	delta, ok, err = AppendGrammarToolInputJSONDelta(buffer, "payload", "日本語", true)
	if !ok || err != nil {
		t.Fatalf("second delta: ok=%v err=%v", ok, err)
	}
	if delta != `語"}` {
		t.Fatalf("second delta = %q", delta)
	}
}
