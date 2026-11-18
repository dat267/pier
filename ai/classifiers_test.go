package ai

import (
	"encoding/json"
	"math"
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

// pi v1.5.0 api/classifier-shared.ts parseClassifierUsage keeps any finite
// positive token count in JavaScript Number space and adds the total as a
// Number. The Go port stores Usage in int64, so it must saturate instead of
// dropping values above the range or wrapping the sum.
func TestClassifierUsageSaturatesHugeFiniteCounts(t *testing.T) {
	model := &Model{Cost: ModelCost{ModelCostRates: ModelCostRates{Input: 1, Output: 2}}}
	tests := []struct {
		name          string
		raw           string
		input, output int64
		total         int64
	}{
		{name: "beyond int64", raw: `{"input_tokens":1e308,"output_tokens":1e308}`,
			input: math.MaxInt64, output: math.MaxInt64, total: math.MaxInt64},
		{name: "sum overflow", raw: `{"input_tokens":6e18,"output_tokens":6e18}`,
			input: 6_000_000_000_000_000_000, output: 6_000_000_000_000_000_000, total: math.MaxInt64},
	}
	parsers := []struct {
		label string
		run   func(json.RawMessage, *Model) *Usage
	}{
		{label: "openai-decisions", run: parseOpenAIDecisionsUsage},
		{label: "system-one", run: parseSystemOneUsage},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			for _, parser := range parsers {
				usage := parser.run(json.RawMessage(test.raw), model)
				if usage == nil {
					t.Fatalf("%s usage was not parsed", parser.label)
				}
				if usage.Input != test.input || usage.Output != test.output || usage.TotalTokens != test.total {
					t.Errorf("%s usage = input %d output %d total %d, want %d %d %d",
						parser.label, usage.Input, usage.Output, usage.TotalTokens, test.input, test.output, test.total)
				}
			}
		})
	}
}
