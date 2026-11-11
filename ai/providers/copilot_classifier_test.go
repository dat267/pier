package providers

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dat267/pier/ai"
)

// pi v1.5.2 (b4afdddf) providers/github-copilot.ts registers
// GITHUB_COPILOT_CLASSIFIER_MODELS with the provider, so a Copilot account can
// pick the System One classifier model (Microsoft-Decision-1) that the catalog
// already ships. Without the registration the model was invisible.
func TestGitHubCopilotOffersItsSystemOneClassifierModels(t *testing.T) {
	provider := GitHubCopilotProvider()
	classifiers := provider.GetClassifierModels()
	if len(classifiers) == 0 {
		t.Fatal("the Copilot provider offers no classifier models")
	}
	var found *ai.Model
	for _, model := range classifiers {
		if model.ID == "microsoft-decision-1" {
			found = model
		}
	}
	if found == nil {
		t.Fatalf("classifier models = %+v; want microsoft-decision-1", classifiers)
	}
	if found.API != ai.APITypeSafeSystemOne {
		t.Fatalf("api = %s", found.API)
	}
}

// pi v1.5.2 providers/github-copilot.ts copilotSystemOne: Copilot serves System
// One at /v1/systemone while its chat APIs have no version prefix, and the login
// replaces the catalog base URL with the account's endpoint, so the prefix is
// added per request.
func TestGitHubCopilotClassifierPrefersTheVersionedPath(t *testing.T) {
	paths := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths <- r.URL.Path
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"answers":{"approved":{"type":"noul","noul":0.95}}}`)
	}))
	defer server.Close()

	provider := GitHubCopilotProvider()
	implementations := provider.GetClassifierImplementations()
	classifier, ok := implementations[ai.APITypeSafeSystemOne]
	if !ok {
		t.Fatalf("classifiers = %v; want typesafe-system-one", implementations)
	}
	model := &ai.Model{
		ID: "microsoft-decision-1", API: ai.APITypeSafeSystemOne,
		Provider: "github-copilot", BaseURL: server.URL, Type: "classifier",
		Input: []string{"text"},
	}
	result := classifier.Classify(model, ai.ClassifierContext{
		State: []byte(`{"text":"hello"}`),
		Questions: []ai.ClassifierQuestion{{
			Name: "approved", Type: "bool", Instructions: "Approve?",
			BoolCriteria: ai.ClassifierBoolCriteria{True: "yes", False: "no"},
		}},
	}, &ai.ClassifierOptions{StreamOptions: ai.StreamOptions{APIKey: "secret"}})
	if result.StopReason != ai.ClassifierStopStop {
		message := ""
		if result.ErrorMessage != nil {
			message = *result.ErrorMessage
		}
		t.Fatalf("classification = %+v (%s)", result, message)
	}
	if path := <-paths; path != "/v1/systemone" {
		t.Fatalf("path = %q, want /v1/systemone", path)
	}
}
