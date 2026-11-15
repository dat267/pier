package highlight

import (
	"encoding/json"
	"os"
	"testing"
)

// lang_order.go is generated from the reference's lib/index.js; it drives
// auto-detection tie-breaking. Keep it in step with the registry: a language
// added or removed without regenerating would silently change which equal-scoring
// language wins.
func TestReferenceLanguageOrderMatchesRegistry(t *testing.T) {
	registered := map[string]bool{}
	registryMu.RLock()
	for name, language := range registry {
		if language.Name == name {
			registered[name] = true
		}
	}
	registryMu.RUnlock()
	listed := map[string]bool{}
	for _, name := range referenceLanguageOrder {
		if listed[name] {
			t.Errorf("%s is listed twice", name)
		}
		listed[name] = true
		if !registered[name] {
			t.Errorf("%s is listed but not registered", name)
		}
	}
	for name := range registered {
		if !listed[name] {
			t.Errorf("%s is registered but missing from lang_order.go; run node scripts/gen_language_order.mjs ./pi", name)
		}
	}
}

// TestReferenceLanguageOrderMatchesThePin compares the generated order with the
// pin's own registration order, which the auto-detection tie-break depends on.
// The registry check above only catches a language added or dropped; a pin bump
// that reorders them would pass it while changing every equal-relevance tie.
func TestReferenceLanguageOrderMatchesThePin(t *testing.T) {
	data, err := os.ReadFile("testdata/language-order/golden.json")
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	var want []string
	if err := json.Unmarshal(data, &want); err != nil {
		t.Fatalf("golden json: %v", err)
	}
	if len(want) != len(referenceLanguageOrder) {
		t.Fatalf("order has %d languages, golden has %d", len(referenceLanguageOrder), len(want))
	}
	for index := range want {
		if referenceLanguageOrder[index] != want[index] {
			t.Fatalf("order[%d] = %q, golden %q (run node scripts/gen_language_order.mjs ./pi)",
				index, referenceLanguageOrder[index], want[index])
		}
	}
}

// TestCSSKeywordListsMatchThePin compares the CSS keyword tables with the
// reference's own lists. A missing or extra tag, media feature, pseudo-class,
// pseudo-element or property changes which tokens the CSS grammar can highlight,
// and nothing else checks those five tables.
func TestCSSKeywordListsMatchThePin(t *testing.T) {
	data, err := os.ReadFile("testdata/css-lists/golden.json")
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	var want struct {
		Tags           []string `json:"tags"`
		MediaFeatures  []string `json:"mediaFeatures"`
		PseudoClasses  []string `json:"pseudoClasses"`
		PseudoElements []string `json:"pseudoElements"`
		Attributes     []string `json:"attributes"`
	}
	if err := json.Unmarshal(data, &want); err != nil {
		t.Fatalf("golden json: %v", err)
	}
	for _, list := range []struct {
		name string
		got  []string
		want []string
	}{
		{"tags", cssTags, want.Tags},
		{"media features", cssMediaFeatures, want.MediaFeatures},
		{"pseudo-classes", cssPseudoClasses, want.PseudoClasses},
		{"pseudo-elements", cssPseudoElements, want.PseudoElements},
		{"attributes", cssAttributes, want.Attributes},
	} {
		if len(list.got) != len(list.want) {
			t.Errorf("%s: %d entries, golden has %d", list.name, len(list.got), len(list.want))
			continue
		}
		for index := range list.want {
			if list.got[index] != list.want[index] {
				t.Errorf("%s[%d] = %q, golden %q", list.name, index, list.got[index], list.want[index])
				break
			}
		}
	}
}
