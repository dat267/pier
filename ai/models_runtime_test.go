package ai

import (
	"context"
	"errors"
	"maps"
	"slices"
	"testing"
	"time"
)

// Port of the auth-resolution and models-runtime subset of
// packages/ai/test/providers.test.ts, plus calculateCost/thinking-level
// units from models.ts. Builtin-catalog cases (models.dev data) await the
// catalog port.

// A lazy setup failure stamps the message with the request start time, not the
// time the setup finally failed (upstream #10549, api/lazy.ts lazyStream).
func TestLazyStreamSetupErrorUsesRequestStartTime(t *testing.T) {
	model := &Model{API: APIOpenAICompletions, Provider: "test", ID: "test"}
	start := time.Now().UnixMilli()
	release := make(chan struct{})
	stream := LazyStream(model, func() (eventSource, error) {
		<-release
		return nil, errors.New("auth failed")
	})
	// Let the wall clock advance well past the request start before the setup
	// fails, so a timestamp taken at failure time cannot fall inside the
	// tolerance below.
	time.Sleep(50 * time.Millisecond)
	close(release)
	result, err := stream.Result(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.StopReason != StopError || result.ErrorMessage == nil || *result.ErrorMessage != "auth failed" {
		t.Fatalf("result = %+v", result)
	}
	if result.Timestamp < start || result.Timestamp > start+10 {
		t.Fatalf("timestamp = %d; want the request start near %d", result.Timestamp, start)
	}
}

// Reference: pi v1.4.0 packages/ai/src/models.ts createProvider.currentModels
// merges by both model type and ID, not ID alone.
func TestProviderDynamicModelsMergeByTypeAndID(t *testing.T) {
	chat := &Model{ID: "same-id", Provider: "p", API: APIOpenAIResponses, Type: "chat"}
	classifier := &Model{ID: "same-id", Provider: "p", API: APIOpenAIDecisions, Type: "classifier"}
	provider := CreateProvider(CreateProviderOptions{
		ID: "p", Auth: ProviderAuth{APIKey: &ApiKeyAuth{Name: "test"}}, Models: []*Model{chat},
		FetchModels: func(*RefreshModelsContext) ([]*Model, error) { return []*Model{classifier}, nil },
	})
	if err := provider.RefreshModels(&RefreshModelsContext{
		AllowNetwork: true, Ctx: context.Background(),
		Publish: func(publication ModelsPublication) bool {
			if publication.Update != nil {
				publication.Update()
			}
			return true
		},
	}); err != nil {
		t.Fatal(err)
	}
	all := provider.GetAllModels()
	if len(all) != 2 || len(provider.GetModels()) != 1 || len(provider.GetClassifierModels()) != 1 {
		t.Fatalf("all=%v chat=%v classifiers=%v", all, provider.GetModels(), provider.GetClassifierModels())
	}
}

// Reference: pi v1.4.0 packages/ai/src/models.ts createProvider.classify.
func TestModelsClassifyReportsMissingClassifierApiImplementation(t *testing.T) {
	model := &Model{ID: "decision", API: APIOpenAIDecisions, Provider: "p", Type: "classifier"}
	provider := CreateProvider(CreateProviderOptions{
		ID: "p", Auth: ProviderAuth{APIKey: &ApiKeyAuth{Name: "test"}}, Models: []*Model{model},
		Classifiers: map[string]ProviderClassifier{"other-api": OpenAIDecisions{}},
	})
	models := CreateModels(nil)
	models.SetProvider(provider)
	result := models.Classify(model, ClassifierContext{}, nil)
	if result.StopReason != ClassifierStopError || result.ErrorMessage == nil || *result.ErrorMessage != `Provider p has no classifier implementation for "openai-decisions"` {
		t.Fatalf("result = %+v", result)
	}
}

// Reference: pi v1.4.0 packages/ai/src/models.ts getAllAvailable() uses
// filterAllModels for credential-specific visibility across model types.
func TestModelsGetAvailableOfTypeUsesAllModelFilter(t *testing.T) {
	credentials := NewInMemoryCredentialStore()
	ctx := context.Background()
	writeCredential := func(credential *Credential) {
		t.Helper()
		if _, err := credentials.Modify(ctx, "openai", func(*Credential) (*Credential, error) { return credential, nil }); err != nil {
			t.Fatal(err)
		}
	}
	chat := &Model{ID: "gpt-6-luna", Provider: "openai", Type: "chat"}
	classifier := &Model{ID: "gpt-6-luna", Provider: "openai", Type: "classifier"}
	provider := CreateProvider(CreateProviderOptions{
		ID: "openai", Auth: ProviderAuth{APIKey: EnvApiKeyAuth("OpenAI", []string{"OPENAI_API_KEY"}), OAuth: &OAuthAuth{}},
		Models: []*Model{chat, classifier},
		FilterAllModels: func(models []*Model, credential *Credential) []*Model {
			if credential == nil || credential.Type != CredentialOAuth {
				return models
			}
			filtered := make([]*Model, 0, len(models))
			for _, model := range models {
				if model.Type != "classifier" {
					filtered = append(filtered, model)
				}
			}
			return filtered
		},
	})
	models := CreateModels(&CreateModelsOptions{Credentials: credentials, AuthContext: &fakeAuthContext{env: map[string]string{"OPENAI_API_KEY": "key"}}})
	models.SetProvider(provider)

	writeCredential(&Credential{Type: CredentialAPIKey, APIKey: &ApiKeyCredential{Key: "key"}})
	available, err := models.GetAvailableOfType(ctx, "classifier", "openai")
	if err != nil || len(available) != 1 || available[0] != classifier {
		t.Fatalf("API-key classifiers = %v, %v", available, err)
	}
	writeCredential(&Credential{Type: CredentialOAuth, OAuth: &OAuthCredential{Access: "chatgpt"}})
	available, err = models.GetAvailableOfType(ctx, "classifier", "openai")
	if err != nil || len(available) != 0 {
		t.Fatalf("OAuth classifiers = %v, %v", available, err)
	}
	all, err := models.GetAllAvailable(ctx, "openai")
	if err != nil || len(all) != 1 || all[0] != chat {
		t.Fatalf("OAuth all-model availability = %v, %v", all, err)
	}
}

func TestCalculateCostWithTiersAndLongCacheWrites(t *testing.T) {
	// Port of models.ts calculateCost: tier selection by total input usage
	// and Anthropic's 2x base input for 1h cache writes.
	model := &Model{
		Cost: ModelCost{ModelCostRates: ModelCostRates{Input: 3, Output: 15, CacheRead: 0.3, CacheWrite: 3.75},
			Tiers: []ModelCostTier{
				{Input: 2, Output: 10, CacheRead: 0.2, CacheWrite: 2.5, InputTokensAbove: 100000},
			}},
	}

	usage := Usage{Input: 200, CacheRead: 50, CacheWrite: 20}
	cost := CalculateCost(model, &usage)
	// Compare with upstream's exact runtime float math (variables, not
	// constants — Go folds constant expressions with exact arithmetic while
	// JS rounds at runtime). cacheWrite is (rate*short + input*2*long) / 1e6.
	in, read, write := 3.0, 0.3, 3.75
	if cost.Input != in/1000000.0*200.0 || cost.CacheRead != read/1000000.0*50.0 {
		t.Fatalf("base cost = %+v", cost)
	}
	// longWrite = 0 in the base case.
	if cost.CacheWrite != (write*20.0+in*2.0*0.0)/1000000.0 {
		t.Fatalf("base cacheWrite cost = %v", cost.CacheWrite)
	}
	// Tier applies when total input usage (input + cacheRead + cacheWrite = 270) exceeds 100k.
	large := Usage{Input: 150000, CacheRead: 0, CacheWrite: 0}
	cost = CalculateCost(model, &large)
	tierIn := 2.0
	if cost.Input != tierIn/1000000.0*150000.0 {
		t.Fatalf("tiered input cost = %v; want %v", cost.Input, tierIn/1000000.0*150000.0)
	}

	oneHour := int64(10)
	longWrite := Usage{Input: 100, CacheWrite: 30, CacheWrite1h: &oneHour}
	cost = CalculateCost(model, &longWrite)
	// shortWrite = 20 at 3.75, longWrite = 10 at 2x base input (3*2).
	want := (write*20.0 + in*2.0*10.0) / 1000000.0
	if cost.CacheWrite != want {
		t.Fatalf("cacheWrite cost = %v; want %v", cost.CacheWrite, want)
	}
}

func TestGetSupportedAndClampThinkingLevels(t *testing.T) {
	// Port of models.ts getSupportedThinkingLevels/clampThinkingLevel.
	nonReasoning := &Model{Reasoning: false}
	if got := GetSupportedThinkingLevels(nonReasoning); len(got) != 1 || got[0] != ThinkOff {
		t.Fatalf("non-reasoning levels = %v", got)
	}

	// Reasoning without a map: all base levels, but xhigh/max require
	// explicit entries.
	plain := &Model{Reasoning: true}
	got := GetSupportedThinkingLevels(plain)
	want := []ModelThinkingLevel{"off", "minimal", "low", "medium", "high"}
	if len(got) != len(want) {
		t.Fatalf("levels = %v; want %v", got, want)
	}

	// null marks a level unsupported.
	limited := &Model{Reasoning: true, ThinkingLevelMap: ThinkingLevelMap{
		ThinkMinimal: nil,
		ThinkXHigh:   new("xhigh"),
	}}
	got = GetSupportedThinkingLevels(limited)
	want = []ModelThinkingLevel{"off", "low", "medium", "high", "xhigh"}
	if len(got) != len(want) {
		t.Fatalf("levels = %v; want %v", got, want)
	}

	// Clamping: requested unsupported level walks to the nearest support.
	// Upstream walks UP through levels first when clamping.
	if got := ClampThinkingLevel(limited, ThinkMinimal); got != ThinkLow {
		t.Fatalf("clamp(minimal) = %s; want low", got)
	}
	// max is unmapped; the walk down lands on the mapped xhigh.
	if got := ClampThinkingLevel(limited, ThinkMax); got != ThinkXHigh {
		t.Fatalf("clamp(max) = %s; want xhigh", got)
	}
	if got := ClampThinkingLevel(nonReasoning, ThinkHigh); got != ThinkOff {
		t.Fatalf("clamp(high) on non-reasoning = %s; want off", got)
	}
}

func TestModelsAreEqual(t *testing.T) {
	a := &Model{ID: "m", Provider: "p"}
	if !ModelsAreEqual(a, &Model{ID: "m", Provider: "p"}) {
		t.Fatal("equal models")
	}
	if ModelsAreEqual(a, &Model{ID: "m", Provider: "q"}) {
		t.Fatal("different provider")
	}
	if ModelsAreEqual(a, &Model{ID: "m", Provider: "p", Type: "classifier"}) {
		t.Fatal("different model type")
	}
	if !ModelsAreEqual(&Model{ID: "m", Provider: "p"}, &Model{ID: "m", Provider: "p", Type: "chat"}) {
		t.Fatal("an explicit chat type equals the default")
	}
	if ModelsAreEqual(nil, a) || ModelsAreEqual(a, nil) {
		t.Fatal("nil is never equal")
	}
}

func TestMergeHeadersCaseInsensitive(t *testing.T) {
	base := ProviderHeaders{"X-Api-Key": new("a"), "Keep": new("1")}
	override := ProviderHeaders{"x-api-key": new("b")}
	merged := MergeHeaders(base, override)
	if len(merged) != 2 {
		t.Fatalf("merged = %v", merged)
	}
	if v := merged["x-api-key"]; v == nil || *v != "b" {
		t.Fatalf("x-api-key = %v", merged["x-api-key"])
	}
	if merged["X-Api-Key"] != nil {
		t.Fatal("case-variant key should be replaced")
	}
	if v := merged["Keep"]; v == nil || *v != "1" {
		t.Fatalf("Keep = %v", merged["Keep"])
	}
	// nil deletes.
	merged = MergeHeaders(base, ProviderHeaders{"keep": nil})
	if _, ok := merged["Keep"]; ok {
		t.Fatal("nil override should delete")
	}
}

// capturingStreams records the options a Models request reaches the provider
// with (the ai-layer counterpart of the coding ModelRuntime contract tests).
type capturingStreams struct {
	stream       func(*Model, TranscriptContext, *StreamOptions) *AssistantMessageEventStream
	streamSimple func(*Model, TranscriptContext, *SimpleStreamOptions) *AssistantMessageEventStream
}

func (c capturingStreams) StreamSimple(m *Model, t TranscriptContext, o *SimpleStreamOptions) *AssistantMessageEventStream {
	return c.streamSimple(m, t, o)
}

func doneStream() *AssistantMessageEventStream {
	stream := NewAssistantMessageEventStream()
	go func() {
		msg := &AssistantMessage{StopReason: StopStop, Usage: Usage{Cost: UsageCost{}}}
		stream.Push(AssistantMessageEvent{Type: EventDone, Reason: StopStop, Message: msg})
		stream.End(&msg)
	}()
	return stream
}

// Reference: pi v1.4.0, packages/ai/src/models.ts getModels filters the
// provider's all-type catalog to chat models. Image and classifier models have
// separate model accessors.
func TestProviderGetModelsReturnsChatTypesOnly(t *testing.T) {
	provider := CreateProvider(CreateProviderOptions{
		ID: "test",
		Models: []*Model{
			{ID: "chat", Type: "chat"},
			{ID: "legacy"},
			{ID: "image", Type: "image"},
			{ID: "classifier", Type: "classifier"},
		},
	})
	var got []string
	for _, model := range provider.GetModels() {
		got = append(got, model.ID)
	}
	want := []string{"chat", "legacy"}
	if !slices.Equal(got, want) {
		t.Fatalf("Provider.GetModels() = %v, want chat models %v", got, want)
	}
}

// Models.StreamSimple must resolve auth into the provider request and forward
// the simple options (reasoning, tool choice, budgets) unchanged, applying the
// header transform exactly once.
func TestModelsStreamSimpleAppliesResolvedAuthAndSimpleOptions(t *testing.T) {
	models := CreateModels(&CreateModelsOptions{Credentials: NewInMemoryCredentialStore()})
	var gotModel *Model
	var gotSimple *SimpleStreamOptions
	provider := CreateProvider(CreateProviderOptions{
		ID: "p",
		Auth: ProviderAuth{APIKey: &ApiKeyAuth{Name: "p", Resolve: func(AuthResolveInput) (*AuthResult, error) {
			return &AuthResult{Auth: ModelAuth{
				APIKey:  "sk",
				Headers: ProviderHeaders{"X-Auth": new("1")},
			}, Source: "test"}, nil
		}}},
		Models: []*Model{{ID: "m", Name: "m", Provider: "p", API: APIOpenAICompletions, Input: []string{"text"}, ContextWindow: 1000, MaxTokens: 100}},
		Single: capturingStreams{streamSimple: func(m *Model, _ TranscriptContext, o *SimpleStreamOptions) *AssistantMessageEventStream {
			gotModel, gotSimple = m, o
			return doneStream()
		}},
	})
	models.SetProvider(provider)
	model := models.GetModel("p", "m")
	if model == nil {
		t.Fatal("model missing")
	}
	toolChoice := ToolChoice("required")
	highBudget := 4096
	transformed := 0
	_, err := models.StreamSimple(model, Context{}, &ModelsSimpleStreamOptions{
		TransformHeaders: func(headers ProviderHeaders) ProviderHeaders {
			transformed++
			out := ProviderHeaders{}
			maps.Copy(out, headers)
			out["X-Transform"] = new("2")
			return out
		},
		Reasoning:       ThinkHigh,
		ToolChoice:      &toolChoice,
		ThinkingBudgets: &ThinkingBudgets{High: &highBudget}}).Result(bgCtx())
	if err != nil {
		t.Fatal(err)
	}
	if gotSimple == nil || gotModel == nil {
		t.Fatal("the provider's StreamSimple never ran")
	}
	if gotSimple.APIKey != "sk" {
		t.Fatalf("apiKey = %q, want resolved sk", gotSimple.APIKey)
	}
	if transformed != 1 {
		t.Fatalf("TransformHeaders ran %d times, want 1", transformed)
	}
	if gotSimple.TransformHeaders != nil {
		t.Fatal("TransformHeaders was not consumed before the provider ran")
	}
	if value := gotSimple.Headers["X-Auth"]; value == nil || *value != "1" {
		t.Fatalf("X-Auth = %v, want 1", value)
	}
	if value := gotSimple.Headers["X-Transform"]; value == nil || *value != "2" {
		t.Fatalf("X-Transform = %v, want 2", value)
	}
	if gotSimple.Reasoning != ThinkHigh {
		t.Fatalf("Reasoning = %q, want %q", gotSimple.Reasoning, ThinkHigh)
	}
	if gotSimple.ToolChoice == nil || *gotSimple.ToolChoice != toolChoice {
		t.Fatalf("ToolChoice = %v, want %q", gotSimple.ToolChoice, toolChoice)
	}
	if gotSimple.ThinkingBudgets == nil || gotSimple.ThinkingBudgets.High == nil || *gotSimple.ThinkingBudgets.High != 4096 {
		t.Fatalf("ThinkingBudgets = %+v, want high 4096", gotSimple.ThinkingBudgets)
	}
}

// D483: the chat registry lists providers sorted by id. The reference returns
// Array.from(this.providers.values()), i.e. registration order (models.ts
// getProviders); a Go map has no order, so the port canonicalizes it.
func TestModelsGetProvidersSortedByID(t *testing.T) {
	registry := CreateModels(nil)
	ids := []string{"zhipu", "openai", "anthropic", "openrouter", "google", "qwen", "xai", "mistral",
		"deepseek", "moonshot", "alibaba", "baidu", "tencent", "nvidia", "cohere", "perplexity"}
	for _, id := range ids {
		registry.SetProvider(CreateProvider(CreateProviderOptions{ID: id}))
	}
	providers := registry.GetProviders()
	if len(providers) != len(ids) {
		t.Fatalf("providers = %d, want %d", len(providers), len(ids))
	}
	for index := 1; index < len(providers); index++ {
		if providers[index-1].ID > providers[index].ID {
			t.Fatalf("providers out of order at %d: %q before %q", index, providers[index-1].ID, providers[index].ID)
		}
	}
}
