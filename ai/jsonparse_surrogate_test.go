package ai

import "testing"

// D457: a lone surrogate has no Go string representation. The reference keeps
// the code unit and `JSON.stringify` writes it back as an escape, so the tool
// receives `{"a":"\ud83d"}`; the port's codec decodes it to U+FFFD. This is the
// same constraint the streaming partial values hit, and it reaches the
// completed arguments too: a model that emits half a surrogate pair in its tool
// arguments hands the tool a replacement character.
//
// TestToolCallArgumentsReplaceLoneSurrogates records the port's behavior.
func TestToolCallArgumentsReplaceLoneSurrogates(t *testing.T) {
	cases := []struct{ input, want string }{
		{`{"a":"\ud83d"}`, `{"a":"` + "\uFFFD" + `"}`},
		{`{"a":"x\ud83dy"}`, `{"a":"x` + "\uFFFD" + `y"}`},
		{`{"a":"\udc00\udc00"}`, `{"a":"` + "\uFFFD\uFFFD" + `"}`},
		// A complete pair is a single rune and survives.
		{`{"a":"\ud83d\ude00"}`, `{"a":"😀"}`},
	}
	for _, testCase := range cases {
		args, errText := ParseToolCallArguments(testCase.input)
		if errText != "" {
			t.Fatalf("%q reported %q", testCase.input, errText)
		}
		if string(args) != testCase.want {
			t.Errorf("%q = %s, want %s (D457)", testCase.input, args, testCase.want)
		}
	}
}
