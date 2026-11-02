package ai

// Untrusted pi-messages SSE events: the wire contentIndex and event shapes are
// validated before they index the partial content array (M122), so no input may
// panic the parser.

import "testing"

func FuzzParsePiMessagesEvent(f *testing.F) {
	for _, seed := range []string{
		"data: {\"type\":\"text_delta\",\"contentIndex\":0,\"delta\":\"x\"}",
		"data: {\"type\":\"done\",\"reason\":\"stop\"}",
		"data: {\"type\":\"toolcall_delta\",\"contentIndex\":-1}",
		"data: {\"type\":\"text_start\",\"contentIndex\":1000000000}",
		"event: x\ndata: {}",
		"data: {not json}",
		"",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		_, _ = parsePiMessagesEvent(raw)
	})
}
