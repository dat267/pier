package ai

import (
	"encoding/json"
	"testing"
)

// FuzzMessageCodec checks the message codecs never panic and that a decoded
// message is a fixed point of the wire codec: marshaling it and parsing it back
// must reproduce the same bytes (sessions persist these).
func FuzzMessageCodec(f *testing.F) {
	seeds := []string{
		`{"role":"user","content":"hi","timestamp":1}`,
		`{"role":"user","content":[{"type":"text","text":"hi"},{"type":"image","data":"aGk=","mimeType":"image/png"}],"timestamp":1}`,
		`{"role":"assistant","content":[{"type":"text","text":"ok"},{"type":"thinking","thinking":"why","thinkingSignature":"s"}],"api":"anthropic-messages","provider":"anthropic","model":"m","usage":{"input":1,"output":2,"cacheRead":0,"cacheWrite":0,"totalTokens":3,"cost":{"input":0,"output":0,"cacheRead":0,"cacheWrite":0,"total":0}},"stopReason":"stop","timestamp":2}`,
		`{"role":"assistant","content":[{"type":"toolCall","id":"c1","name":"read","arguments":{"path":"a"}}],"api":"openai-responses","provider":"openai","model":"m","usage":{"input":0,"output":0,"cacheRead":0,"cacheWrite":0,"totalTokens":0,"cost":{"input":0,"output":0,"cacheRead":0,"cacheWrite":0,"total":0}},"stopReason":"toolUse","timestamp":3}`,
		`{"role":"toolResult","toolCallId":"c1","toolName":"read","content":[{"type":"text","text":"out"}],"isError":false,"timestamp":4}`,
		`{"role":"system","content":"prompt","timestamp":0}`,
		`{"role":"custom","content":{"x":1},"timestamp":0}`,
		`{}`,
		`null`,
		`{"role":"user","content":`,
		`[1,2,3]`,
	}
	for _, seed := range seeds {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, input string) {
		message, err := UnmarshalMessage(json.RawMessage(input))
		if err != nil {
			return
		}
		encoded, err := MarshalMessage(message)
		if err != nil {
			t.Fatalf("MarshalMessage failed for %q: %v", input, err)
		}
		again, err := UnmarshalMessage(encoded)
		if err != nil {
			t.Fatalf("re-parse of %q failed: %v", encoded, err)
		}
		reencoded, err := MarshalMessage(again)
		if err != nil || string(reencoded) != string(encoded) {
			t.Fatalf("message codec is not a fixed point for %q:\n first = %q\n again = %q (%v)", input, encoded, reencoded, err)
		}
	})
}
