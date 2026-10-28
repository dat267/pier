package highlight

import "testing"

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
