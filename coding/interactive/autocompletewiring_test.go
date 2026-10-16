package interactive

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/dat267/pier/ai"
	"github.com/dat267/pier/coding"
)

// Tests for the autocomplete wiring. The fd-backed file completion is dormant
// unless the wiring resolves the binary: a `FdPath` left empty made every
// `@`-mention completion return nothing, silently.

// The regression: the wiring must pass a resolved fd path through. Without it
// the provider's fd-backed paths short-circuit to nil.
func TestNewAutocompleteWiringResolvesFdPath(t *testing.T) {
	want := ResolveAutocompleteFdPath()
	wiring := newAutocompleteWiring(&App{})
	if wiring.FdPath != want {
		t.Fatalf("FdPath = %q, want %q — the fd-backed file completion is dead without it", wiring.FdPath, want)
	}
	if want == "" {
		t.Skip("fd is not installed; the fd-backed paths stay disabled")
	}
	if _, err := os.Stat(want); err != nil {
		t.Errorf("resolved fd path %q is not usable: %v", want, err)
	}
}

type scopedModelsCountingSession struct {
	AutocompleteSession
	models     []coding.ScopedModel
	calls      int
	copyModels bool
	revision   uint64
}

func (s *scopedModelsCountingSession) ScopedModelsRevision() uint64 { return s.revision }

func (s *scopedModelsCountingSession) ScopedModels() []coding.ScopedModel {
	s.calls++
	if s.copyModels {
		return append([]coding.ScopedModel(nil), s.models...)
	}
	return s.models
}

func TestModelArgumentCompletionReadsScopedModelsOnce(t *testing.T) {
	session := &scopedModelsCountingSession{
		AutocompleteSession: &autocompleteTestSession{},
		models:              []coding.ScopedModel{{Model: &ai.Model{Provider: "test", ID: "test-model"}}},
	}
	provider := (&AutocompleteWiring{Session: session}).CreateBaseAutocompleteProvider()
	suggestions := provider.GetSuggestions(context.Background(), []string{"/model "}, 0, len("/model "), false)
	if suggestions == nil || len(suggestions.Items) != 1 {
		t.Fatalf("model argument suggestions = %+v", suggestions)
	}
	if session.calls != 1 {
		t.Fatalf("ScopedModels calls = %d, want 1", session.calls)
	}
}

// The resolved path must be the one the provider actually receives.
func TestBaseAutocompleteProviderUsesTheResolvedFdPath(t *testing.T) {
	fdPath := ResolveAutocompleteFdPath()
	if fdPath == "" {
		t.Skip("fd is not installed")
	}

	cwd := t.TempDir()
	if err := os.WriteFile(filepath.Join(cwd, "visible.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(cwd, ".pi"), 0o755); err != nil {
		t.Fatal(err)
	}

	wiring := &AutocompleteWiring{
		Session:     &autocompleteTestSession{},
		SessionInfo: coding.NewSessionManager(cwd, nil),
		FdPath:      fdPath,
	}
	provider := wiring.CreateBaseAutocompleteProvider()

	suggestions := provider.GetSuggestions(context.Background(), []string{"@"}, 0, 1, true)
	if suggestions == nil || len(suggestions.Items) == 0 {
		t.Fatal("@-mention completion returned nothing with fd configured")
	}

	labels := map[string]bool{}
	for _, item := range suggestions.Items {
		labels[item.Label] = true
	}
	if !labels["visible.txt"] {
		t.Errorf("@ suggestions = %v, want the visible file", labels)
	}
	// Hidden entries are not filtered: fd runs with --hidden.
	if !labels[".pi/"] {
		t.Errorf("@ suggestions = %v, want the hidden directory too (fd runs with --hidden)", labels)
	}
}

// ModelRuntime is optional for an AgentSession with no catalog.
func TestModelArgumentCompletionWithoutRuntimeReturnsNoSuggestions(t *testing.T) {
	provider := (&AutocompleteWiring{Session: &autocompleteTestSession{}}).CreateBaseAutocompleteProvider()
	if suggestions := provider.GetSuggestions(context.Background(), []string{"/model "}, 0, len("/model "), false); suggestions != nil {
		t.Fatalf("model suggestions without a runtime = %+v, want none", suggestions)
	}
}

func TestModelArgumentCompletionAllocationBudget(t *testing.T) {
	const modelCount = 4096
	model := &ai.Model{Provider: "p", ID: "m"}
	models := make([]coding.ScopedModel, modelCount)
	for index := range models {
		models[index] = coding.ScopedModel{Model: model}
	}
	session := &scopedModelsCountingSession{
		AutocompleteSession: &autocompleteTestSession{},
		models:              models,
	}
	provider := (&AutocompleteWiring{Session: session}).CreateBaseAutocompleteProvider()

	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	suggestions := provider.GetSuggestions(context.Background(), []string{"/model "}, 0, len("/model "), false)
	runtime.ReadMemStats(&after)

	if suggestions == nil {
		t.Fatal("model argument suggestions are nil")
	}
	if len(suggestions.Items) != modelCount {
		t.Fatalf("model argument suggestions = %d, want %d", len(suggestions.Items), modelCount)
	}
	allocated := after.TotalAlloc - before.TotalAlloc
	t.Logf("completing %d scoped models allocated %d bytes", modelCount, allocated)
	if allocated > uint64(modelCount*120) {
		t.Fatalf("completing %d scoped models allocated %d bytes, want less than %d", modelCount, allocated, modelCount*120)
	}
}

func TestModelArgumentCompletionReturnsOnlyMatchingModel(t *testing.T) {
	const modelCount = 4096
	common := &ai.Model{Provider: "test", ID: "model"}
	models := make([]coding.ScopedModel, modelCount)
	for index := range models {
		models[index] = coding.ScopedModel{Model: common}
	}
	models[modelCount/2] = coding.ScopedModel{Model: &ai.Model{Provider: "test", ID: "model", Name: "needle"}}
	session := &scopedModelsCountingSession{
		AutocompleteSession: &autocompleteTestSession{},
		models:              models,
	}
	provider := (&AutocompleteWiring{Session: session}).CreateBaseAutocompleteProvider()
	suggestions := provider.GetSuggestions(context.Background(), []string{"/model needle"}, 0, len("/model needle"), false)
	if suggestions == nil || len(suggestions.Items) != 1 || suggestions.Items[0].Value != "test/model" {
		t.Fatalf("model argument suggestions = %+v, want the matching model", suggestions)
	}
}

func TestModelArgumentCompletionReusesSearchItemsBetweenKeystrokes(t *testing.T) {
	const modelCount = 4096
	common := &ai.Model{Provider: "test", ID: "model"}
	models := make([]coding.ScopedModel, modelCount)
	for index := range models {
		models[index] = coding.ScopedModel{Model: common}
	}
	models[modelCount/2] = coding.ScopedModel{Model: &ai.Model{Provider: "test", ID: "model", Name: "needle"}}
	session := &scopedModelsCountingSession{
		AutocompleteSession: &autocompleteTestSession{},
		models:              models,
		copyModels:          true,
	}
	provider := (&AutocompleteWiring{Session: session}).CreateBaseAutocompleteProvider()
	if result := provider.GetSuggestions(context.Background(), []string{"/model needle"}, 0, len("/model needle"), false); result == nil || len(result.Items) != 1 {
		t.Fatalf("initial model suggestions = %+v, want one match", result)
	}

	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	result := provider.GetSuggestions(context.Background(), []string{"/model needle"}, 0, len("/model needle"), false)
	runtime.ReadMemStats(&after)

	if result == nil || len(result.Items) != 1 || result.Items[0].Value != "test/model" {
		t.Fatalf("cached model suggestions = %+v, want the matching model", result)
	}
	if session.calls != 1 {
		t.Fatalf("ScopedModels calls = %d after cached completion, want 1", session.calls)
	}
	allocated := after.TotalAlloc - before.TotalAlloc
	t.Logf("cached completion over %d models allocated %d bytes", modelCount, allocated)
	if allocated > 16*1024 {
		t.Fatalf("cached completion over %d models allocated %d bytes, want less than %d", modelCount, allocated, 16*1024)
	}
}

func TestModelArgumentCompletionEmptyPrefixReusesCachedScopeItems(t *testing.T) {
	const modelCount = 4096
	model := &ai.Model{Provider: "test", ID: "model"}
	models := make([]coding.ScopedModel, modelCount)
	for index := range models {
		models[index] = coding.ScopedModel{Model: model}
	}
	session := &scopedModelsCountingSession{
		AutocompleteSession: &autocompleteTestSession{},
		models:              models,
		copyModels:          true,
	}
	provider := (&AutocompleteWiring{Session: session}).CreateBaseAutocompleteProvider()
	if result := provider.GetSuggestions(context.Background(), []string{"/model m"}, 0, len("/model m"), false); result == nil || len(result.Items) != modelCount {
		t.Fatalf("initial model suggestions = %+v, want %d items", result, modelCount)
	}

	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	result := provider.GetSuggestions(context.Background(), []string{"/model "}, 0, len("/model "), false)
	runtime.ReadMemStats(&after)

	if result == nil || len(result.Items) != modelCount {
		t.Fatalf("empty-prefix model suggestions = %+v, want %d items", result, modelCount)
	}
	allocated := after.TotalAlloc - before.TotalAlloc
	t.Logf("empty-prefix completion over %d cached models allocated %d bytes", modelCount, allocated)
	if allocated > uint64(modelCount*55) {
		t.Fatalf("empty-prefix completion allocated %d bytes, want less than %d", allocated, modelCount*55)
	}
	if session.calls != 1 {
		t.Fatalf("ScopedModels calls = %d for a cached empty-prefix completion, want 1", session.calls)
	}
}

func TestModelArgumentCompletionRefreshesChangedScopedModels(t *testing.T) {
	first := &ai.Model{Provider: "test", ID: "first", Name: "needle"}
	second := &ai.Model{Provider: "test", ID: "second", Name: "needle"}
	session := &scopedModelsCountingSession{
		AutocompleteSession: &autocompleteTestSession{},
		models:              []coding.ScopedModel{{Model: first}},
	}
	provider := (&AutocompleteWiring{Session: session}).CreateBaseAutocompleteProvider()
	query := []string{"/model needle"}
	firstResult := provider.GetSuggestions(context.Background(), query, 0, len(query[0]), false)
	if firstResult == nil || len(firstResult.Items) != 1 || firstResult.Items[0].Value != "test/first" {
		t.Fatalf("initial suggestions = %+v, want first model", firstResult)
	}

	session.models = []coding.ScopedModel{{Model: second}}
	session.revision++
	secondResult := provider.GetSuggestions(context.Background(), query, 0, len(query[0]), false)
	if secondResult == nil || len(secondResult.Items) != 1 || secondResult.Items[0].Value != "test/second" {
		t.Fatalf("updated suggestions = %+v, want second model", secondResult)
	}
}

// The in-process path completion (no fd involved) offers hidden entries after
// `~/` without the leading dot being typed.
func TestPathCompletionOffersHiddenEntries(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".pi"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "notes.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)

	provider := (&AutocompleteWiring{
		Session:     &autocompleteTestSession{},
		SessionInfo: coding.NewSessionManager(t.TempDir(), nil),
	}).CreateBaseAutocompleteProvider()
	suggestions := provider.GetSuggestions(context.Background(), []string{"~/"}, 0, 2, true)
	if suggestions == nil {
		t.Fatal("~/ completion returned nothing")
	}

	labels := map[string]bool{}
	for _, item := range suggestions.Items {
		labels[item.Label] = true
	}
	if !labels[".pi/"] {
		t.Errorf("~/ suggestions = %v, want the hidden directory", labels)
	}
}

// End to end through the real app wiring: the app's own autocomplete provider
// must answer an @-mention query. This is the assertion that would have caught
// the wiring gap, since it never touches the provider constructor directly.
func TestAppAutocompleteCompletesAtMentions(t *testing.T) {
	if ResolveAutocompleteFdPath() == "" {
		t.Skip("fd is not installed")
	}
	app, cleanup := newTestApp(t)
	defer cleanup()

	if err := os.WriteFile(filepath.Join(app.options.Cwd, "wired.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(app.options.Cwd, ".hidden-dir"), 0o755); err != nil {
		t.Fatal(err)
	}

	// The boot path installs the provider.
	app.autocomplete.SetupAutocompleteProvider()
	provider := app.autocomplete.Provider()
	if provider == nil {
		t.Fatal("the app installed no autocomplete provider")
	}

	suggestions := provider.GetSuggestions(context.Background(), []string{"@"}, 0, 1, true)
	if suggestions == nil || len(suggestions.Items) == 0 {
		t.Fatal("the app's provider returned nothing for @ — fd is still not wired through")
	}
	labels := map[string]bool{}
	for _, item := range suggestions.Items {
		labels[item.Label] = true
	}
	if !labels["wired.txt"] {
		t.Errorf("@ suggestions = %v, want the file in the session cwd", labels)
	}
	if !labels[".hidden-dir/"] {
		t.Errorf("@ suggestions = %v, want the hidden directory (fd runs with --hidden)", labels)
	}
}

// The built-in /goal command completes its subcommands (upstream extension's
// getArgumentCompletions).
func TestGoalCommandArgumentCompletions(t *testing.T) {
	wiring := &AutocompleteWiring{Session: &autocompleteTestSession{}}
	provider := wiring.CreateBaseAutocompleteProvider()

	suggestions := provider.GetSuggestions(context.Background(), []string{"/goal "}, 0, 6, false)
	if suggestions == nil {
		t.Fatal("/goal offered no argument completions")
	}
	values := map[string]bool{}
	for _, item := range suggestions.Items {
		values[item.Value] = true
	}
	for _, want := range []string{"status", "set", "pause", "resume", "clear"} {
		if !values[want] {
			t.Errorf("missing /goal subcommand %q in %v", want, values)
		}
	}

	narrowed := provider.GetSuggestions(context.Background(), []string{"/goal p"}, 0, 7, false)
	if narrowed == nil || len(narrowed.Items) != 1 || narrowed.Items[0].Value != "pause" {
		t.Fatalf("/goal p = %+v, want just pause", narrowed)
	}
}
