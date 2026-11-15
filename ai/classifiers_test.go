package ai

import (
	"strings"
	"testing"
)

// pi v1.5.2 packs ClassifierAnswer and ClassifierResult as plain object
// literals, so JSON.stringify writes their keys in the order the API code
// writes them: the three answer shapes in their interface order, and the result
// as api, provider, model, answers, stopReason, timestamp, then usage when the
// service billed, then errorMessage on failure (api/openai-decisions.ts and
// api/system-one-shared.ts).
func TestClassifierAnswerAndResultKeyOrder(t *testing.T) {
	confidence := 0.8
	score := 2.0
	probability := 0.95

	choice := ClassifierAnswer{
		Type: "choice", Choice: "ok",
		Probabilities:    map[string]float64{"a": 0.1, "b": 0.9},
		ProbabilityOrder: []string{"b", "a"},
		Confidence:       &confidence,
	}
	encoded, err := MarshalJSON(choice)
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"type":"choice","choice":"ok","probabilities":{"b":0.9,"a":0.1},"confidence":0.8}`; string(encoded) != want {
		t.Fatalf("choice = %s, want %s", encoded, want)
	}
	// Integer-like probability keys lead in numeric order, as Object.keys does.
	indexed := ClassifierAnswer{
		Type: "choice", Choice: "10",
		Probabilities: map[string]float64{"10": 0.5, "2": 0.5},
		Confidence:    &confidence,
	}
	encoded, err = MarshalJSON(indexed)
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"type":"choice","choice":"10","probabilities":{"2":0.5,"10":0.5},"confidence":0.8}`; string(encoded) != want {
		t.Fatalf("indexed = %s, want %s", encoded, want)
	}

	for _, answer := range []ClassifierAnswer{
		{Type: "score", Score: &score, Confidence: &confidence},
		{Type: "bool", Probability: &probability},
	} {
		encoded, err := MarshalJSON(answer)
		if err != nil {
			t.Fatal(err)
		}
		want := `{"type":"score","score":2,"confidence":0.8}`
		if answer.Type == "bool" {
			want = `{"type":"bool","probability":0.95}`
		}
		if string(encoded) != want {
			t.Fatalf("%s = %s, want %s", answer.Type, encoded, want)
		}
	}

	errorMessage := "boom"
	result := ClassifierResult{
		API: "openai-decisions", Provider: "typesafe", Model: "m",
		Answers:      map[string]ClassifierAnswer{"q": choice},
		AnswerOrder:  []string{"q"},
		StopReason:   ClassifierStopStop,
		Timestamp:    123,
		ErrorMessage: &errorMessage,
	}
	encoded, err = MarshalJSON(result)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(encoded), `{"api":"openai-decisions","provider":"typesafe","model":"m","answers":{"q":`) {
		t.Fatalf("result prefix = %s", encoded)
	}
	if !strings.Contains(string(encoded), `"stopReason":"stop","timestamp":123,"errorMessage":"boom"}`) {
		t.Fatalf("result tail = %s", encoded)
	}

	result.ErrorMessage = nil
	usage := Usage{Cost: UsageCost{Input: 1, Output: 2, Total: 3}}
	result.Usage = &usage
	encoded, err = MarshalJSON(result)
	if err != nil {
		t.Fatal(err)
	}
	timestampIndex := strings.Index(string(encoded), `"timestamp":123`)
	usageIndex := strings.Index(string(encoded), `"usage":`)
	if timestampIndex < 0 || usageIndex < timestampIndex {
		t.Fatalf("usage does not follow timestamp: %s", encoded)
	}
}
