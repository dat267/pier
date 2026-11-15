package ai

import (
	"encoding/json"
	"strings"
	"testing"
)

// Reference: pi v1.5.2 (b4afdddf) serializes a model with JSON.stringify, which
// escapes only the characters JSON requires; a compat string such as a
// chat-template kwarg keeps "<", ">" and "&" as written. Pier encoded the compat
// arm with encoding/json, which writes them as \u003c, \u003e and \u0026.
func TestModelCompatJSONKeepsHTMLCharactersLiteral(t *testing.T) {
	kwargs := map[string]json.RawMessage{"prefix": json.RawMessage(`"<b>a & b</b>"`)}
	model := Model{
		ID: "m", Name: "M", API: APIOpenAICompletions, Provider: "p",
		BaseURL: "https://example.test", Input: []string{"text"},
		Compat: &ModelCompat{OpenAICompletions: &OpenAICompletionsCompat{ChatTemplateKwargs: kwargs}},
	}
	encoded, err := model.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"<b>a & b</b>"`) {
		t.Fatalf("compat string was escaped: %s", encoded)
	}
	if strings.Contains(string(encoded), `\u003c`) {
		t.Fatalf("compat string used HTML escaping: %s", encoded)
	}
}
