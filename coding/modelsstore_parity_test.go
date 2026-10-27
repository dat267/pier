package coding

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/dat267/pier/ai"
)

// pi v1.4.0 (4a080252), core/remote-catalog-provider.ts parseCatalog and
// core/models-store.ts retain compat; api/openai-completions.ts getCompat
// honors supportsReasoningEffort even when it is the only override.
func TestRemoteCatalogCompatSurvivesModelStore(t *testing.T) {
	for name, store := range parityModelsStores(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			base := ai.CreateProvider(ai.CreateProviderOptions{ID: "p"})
			provider := WithRemoteCatalog(base, "https://catalog.invalid", 0, false, func(context.Context, *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`[{"id":"m","api":"openai-completions","baseUrl":"https://provider.invalid/v1","reasoning":true,"maxTokens":1024,"contextWindow":8192,"compat":{"supportsReasoningEffort":false}}]`))}, nil
			})
			if err := provider.RefreshModels(&ai.RefreshModelsContext{Ctx: ctx, AllowNetwork: true, Publish: func(p ai.ModelsPublication) bool {
				if p.Update != nil {
					p.Update()
				}
				if p.Persist != nil {
					if err := store.Write("p", p.Persist, ctx); err != nil {
						t.Fatal(err)
					}
				}
				return true
			}}); err != nil {
				t.Fatal(err)
			}
			saved, err := store.Read("p", ctx)
			if err != nil {
				t.Fatal(err)
			}
			if saved == nil || len(saved.Models) != 1 {
				t.Fatalf("saved = %+v", saved)
			}
			for _, model := range []*ai.Model{provider.GetModels()[0], saved.Models[0]} {
				if ai.GetOpenAICompletionsCompat(model).SupportsReasoningEffort {
					t.Fatal("supportsReasoningEffort=false was lost")
				}
			}
			if body := parityCompletionsBody(t, saved.Models[0]); strings.Contains(body, `"reasoning_effort"`) {
				t.Fatalf("request must omit reasoning_effort: %s", body)
			}
		})
	}
}

// Source-generated pi v1.4.0 (4a080252) models-store.ts rewrite fixtures.
// Regenerate: node coding/testdata/models-store/generate.mjs ./pi > coding/testdata/models-store/golden.json
func TestModelsStorePreservesSharedImageModels(t *testing.T) {
	raw, err := os.ReadFile("testdata/models-store/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Operation  string `json:"operation"`
		Initial    string `json:"initial"`
		ModelJSON  string `json:"modelJSON"`
		CheckedAt  int64  `json:"checkedAt"`
		Generation struct {
			StopReason  string   `json:"stopReason"`
			OutputTypes []string `json:"outputTypes"`
		} `json:"generation"`
	}
	if err := json.Unmarshal(raw, &fixtures); err != nil {
		t.Fatal(err)
	}
	for _, fixture := range fixtures {
		t.Run(fixture.Operation, func(t *testing.T) {
			ctx := context.Background()
			path := filepath.Join(t.TempDir(), "models-store.json")
			if err := os.WriteFile(path, []byte(fixture.Initial), 0o600); err != nil {
				t.Fatal(err)
			}
			store := NewFileModelsStore(path)
			switch fixture.Operation {
			case "write-other":
				checkedAt := int64(456)
				err = store.Write("other", &ai.ModelsStoreEntry{Models: []*ai.Model{}, CheckedAt: &checkedAt}, ctx)
			case "delete-other":
				err = store.Delete("other", ctx)
			case "update-metadata":
				stored, readErr := store.Read("openrouter", ctx)
				if readErr != nil {
					t.Fatal(readErr)
				}
				checkedAt := int64(456)
				stored.CheckedAt = &checkedAt
				err = store.Write("openrouter", stored, ctx)
			case "edit-model":
				stored, readErr := store.Read("openrouter", ctx)
				if readErr != nil {
					t.Fatal(readErr)
				}
				stored.Models[0].Name = "Renamed"
				stored.Models[0].Headers = nil
				err = store.Write("openrouter", stored, ctx)
			default:
				t.Fatalf("unknown operation %q", fixture.Operation)
			}
			if err != nil {
				t.Fatal(err)
			}
			disk, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var providers map[string]struct {
				Models    []json.RawMessage `json:"models"`
				CheckedAt int64             `json:"checkedAt"`
			}
			if err := json.Unmarshal(disk, &providers); err != nil {
				t.Fatal(err)
			}
			entry := providers["openrouter"]
			if entry.CheckedAt != fixture.CheckedAt {
				t.Fatalf("checkedAt = %d, want %d", entry.CheckedAt, fixture.CheckedAt)
			}
			if len(entry.Models) != 1 {
				t.Fatalf("models = %s", disk)
			}
			modelJSON, err := ai.MarshalJSON(entry.Models[0])
			if err != nil {
				t.Fatal(err)
			}
			if string(modelJSON) != fixture.ModelJSON {
				t.Fatalf("image model JSON = %s, want %s", modelJSON, fixture.ModelJSON)
			}
			var image ai.ImagesModel
			if err := json.Unmarshal(modelJSON, &image); err != nil {
				t.Fatal(err)
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, `{"id":"r","choices":[{"message":{"content":null,"images":[{"image_url":{"url":"data:image/png;base64,AA=="}}]}}]}`)
			}))
			defer server.Close()
			image.BaseURL = server.URL
			result := (ai.OpenRouterImages{}).GenerateImages(&image, ai.ImagesContext{Input: []ai.ImagesInputContent{ai.TextContent{Text: "draw a dot"}}}, &ai.ImagesOptions{APIKey: "mock-key"})
			if result.StopReason != fixture.Generation.StopReason {
				t.Fatalf("generation failed: %+v", result)
			}
			types := []string{}
			for _, block := range result.Output {
				switch block.(type) {
				case ai.ImageContent:
					types = append(types, "image")
				case ai.TextContent:
					types = append(types, "text")
				default:
					t.Fatalf("unexpected output %T", block)
				}
			}
			if !reflect.DeepEqual(types, fixture.Generation.OutputTypes) {
				t.Fatalf("output types = %v, want %v", types, fixture.Generation.OutputTypes)
			}
		})
	}
}

func parityModelsStores(t *testing.T) map[string]ai.ModelsStore {
	t.Helper()
	return map[string]ai.ModelsStore{
		"ai-memory":     ai.NewInMemoryModelsStore(),
		"coding-memory": NewInMemoryCodingAgentModelsStore(),
		"file":          NewFileModelsStore(filepath.Join(t.TempDir(), "models-store.json")),
	}
}

func parityCompletionsBody(t *testing.T, model *ai.Model) string {
	t.Helper()
	bodies := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
			return
		}
		bodies <- string(raw)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"id\":\"r\",\"choices\":[{\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	defer server.Close()
	copy := *model
	copy.BaseURL = server.URL + "/v1"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	zero := 0
	result, err := ai.StreamOpenAICompletions(&copy, ai.TranscriptContext{Messages: []ai.Message{&ai.UserMessage{Content: ai.StringOrBlocks{Text: "hi"}, Timestamp: 1}}}, &ai.OpenAICompletionsOptions{Ctx: ctx, APIKey: "mock-key", MaxRetries: &zero, ReasoningEffort: "high"}).Result(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if result.ErrorMessage != nil {
		t.Fatal(*result.ErrorMessage)
	}
	select {
	case body := <-bodies:
		return body
	case <-ctx.Done():
		t.Fatal(ctx.Err())
		return ""
	}
}

// pi v1.4.0 (4a080252), core/models-store.ts structuredClone/JSON.stringify
// preserve sampling-parameter order, which api/openai-completions.ts buildParams
// carries into the HTTP body through Object.assign(params, samplingParams).
func TestModelsStorePreservesSamplingOrder(t *testing.T) {
	for name, store := range parityModelsStores(t) {
		t.Run(name, func(t *testing.T) {
			var model ai.Model
			if err := json.Unmarshal([]byte(`{"id":"m","api":"openai-completions","provider":"p","reasoning":true,"maxTokens":1024,"contextWindow":8192,"samplingParams":{"top_k":5,"seed":1},"samplingParamsByThinkingLevel":{"high":{"z_option":3,"a_option":4}}}`), &model); err != nil {
				t.Fatal(err)
			}
			ctx := context.Background()
			if err := store.Write("p", &ai.ModelsStoreEntry{Models: []*ai.Model{&model}}, ctx); err != nil {
				t.Fatal(err)
			}
			saved, err := store.Read("p", ctx)
			if err != nil {
				t.Fatal(err)
			}
			if saved == nil || len(saved.Models) != 1 {
				t.Fatalf("saved = %+v", saved)
			}
			encoded, err := ai.MarshalJSON(saved.Models[0])
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range []string{`"samplingParams":{"top_k":5,"seed":1}`, `"high":{"z_option":3,"a_option":4}`} {
				if !strings.Contains(string(encoded), want) {
					t.Errorf("model JSON must contain %s: %s", want, encoded)
				}
			}
			if body := parityCompletionsBody(t, saved.Models[0]); !strings.Contains(body, `"top_k":5,"seed":1,"z_option":3,"a_option":4`) {
				t.Errorf("sampling wire order changed: %s", body)
			}
		})
	}
}
