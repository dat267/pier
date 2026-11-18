package ai

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"unsafe"
)

// Port of packages/ai/test/text.test.ts.

func testContent() []Content {
	return []Content{
		ThinkingContent{Thinking: "reasoning"},
		TextContent{Text: "first"},
		ToolCall{ID: "1", Name: "read", Arguments: json.RawMessage(`{}`)},
		TextContent{Text: "second"},
	}
}

func TestContentTextExtractsAssistantTextBlocks(t *testing.T) {
	got := ContentText(StringOrBlocks{Blocks: testContent()}, "\n")
	if got != "first\nsecond" {
		t.Fatalf("got %q; want %q", got, "first\nsecond")
	}
}

func TestContentTextSupportsCustomSeparators(t *testing.T) {
	got := ContentText(StringOrBlocks{Blocks: testContent()}, "")
	if got != "firstsecond" {
		t.Fatalf("got %q; want %q", got, "firstsecond")
	}
}

func TestContentTextPassesStringThrough(t *testing.T) {
	got := ContentText(StringOrBlocks{Text: "hello"}, "\n")
	if got != "hello" {
		t.Fatalf("got %q; want %q", got, "hello")
	}
}

func TestContentTextExtractsTextFromToolResultContent(t *testing.T) {
	content := ContentList{
		TextContent{Text: "first"},
		ImageContent{Data: "...", MimeType: "image/png"},
		TextContent{Text: "second"},
	}
	got := ContentText(StringOrBlocks{Blocks: content}, "")
	if got != "firstsecond" {
		t.Fatalf("got %q; want %q", got, "firstsecond")
	}
}

// TestStringThinkingNullSendsTheLevelName covers upstream's
// `thinking = map?.[effort] ?? effort` (openai-completions.ts 949-950): a null
// mapping must send the level name, not the "none" off-marker.
func TestStringThinkingNullSendsTheLevelName(t *testing.T) {
	explicitNull := (*string)(nil)
	model := &Model{
		ID: "m", Name: "m", API: APIOpenAICompletions, Provider: "custom",
		BaseURL: "https://invalid.test/v1", Reasoning: true, Input: []string{"text"},
		ContextWindow: 1000, MaxTokens: 100,
		ThinkingLevelMap: ThinkingLevelMap{ThinkOff: new("none"), ThinkMedium: explicitNull},
		Compat: &ModelCompat{OpenAICompletions: &OpenAICompletionsCompat{
			ThinkingFormat: new("string-thinking"),
		}},
	}
	params, err := BuildOpenAICompletionsParams(model, NormalizeContext(Context{}), &OpenAICompletionsOptions{
		ReasoningEffort: ThinkMedium,
	}, nil, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := MarshalJSON(params)
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Thinking *string `json:"thinking"`
	}
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Thinking == nil || *decoded.Thinking != "medium" {
		t.Fatalf("thinking = %v, want medium; body %s", decoded.Thinking, encoded)
	}
	// The off branch keeps the "none" fallback for a missing off mapping.
	off := &Model{ID: "m", API: APIOpenAICompletions, Provider: "custom",
		BaseURL: "https://invalid.test/v1", Reasoning: true,
		ThinkingLevelMap: ThinkingLevelMap{}, Compat: model.Compat}
	params, err = BuildOpenAICompletionsParams(off, NormalizeContext(Context{}), nil, nil, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err = MarshalJSON(params)
	if err != nil {
		t.Fatal(err)
	}
	if want := `"thinking":"none"`; !strings.Contains(string(encoded), want) {
		t.Fatalf("off body %s, want %s", encoded, want)
	}
}

// pi v1.4.0 (4a080252), ai/src/utils/hash.ts shortHash.
// Regenerate:
//
//	node --experimental-strip-types ai/testdata/short-hash/generate.mjs ./pi > ai/testdata/short-hash/golden.json
func TestShortHashMatchesUpstream(t *testing.T) {
	raw, err := os.ReadFile("testdata/short-hash/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Input    string `json:"input"`
		Expected string `json:"expected"`
	}
	if err := json.Unmarshal(raw, &fixtures); err != nil {
		t.Fatal(err)
	}
	if len(fixtures) == 0 {
		t.Fatal("empty short-hash corpus")
	}
	for _, fixture := range fixtures {
		if got := ShortHash(fixture.Input); got != fixture.Expected {
			t.Errorf("ShortHash(%q) = %q, want %q", fixture.Input, got, fixture.Expected)
		}
	}
}

// Reference: pi v1.5.0 utils/text.ts ownedSlice. Go substrings share the
// backing array, so a 500-byte error excerpt of a megabyte body would retain
// the whole body; the truncation sites must copy.
func TestTruncatedErrorTextOwnsItsBytes(t *testing.T) {
	body := strings.Repeat("x", 1<<20) + "tail"
	truncated := TruncateErrorText(body, 500)
	if !strings.HasPrefix(truncated, strings.Repeat("x", 500)) {
		t.Fatalf("truncated prefix = %q", truncated[:20])
	}
	if unsafe.StringData(truncated) == unsafe.StringData(body) {
		t.Fatal("TruncateErrorText returned a view into the full body")
	}

	diagnosticSource := strings.Repeat("y", 20000)
	diagnostic := truncateDiagnosticString(diagnosticSource)
	if unsafe.StringData(diagnostic) == unsafe.StringData(diagnosticSource) {
		t.Fatal("diagnostic truncation returned a view")
	}
	httpSource := strings.Repeat("z", 20000)
	if got := truncateHTTPBody(httpSource); unsafe.StringData(got) == unsafe.StringData(httpSource) {
		t.Fatal("HTTP body truncation returned a view")
	}
}
