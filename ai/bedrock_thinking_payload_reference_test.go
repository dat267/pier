package ai

import (
	"reflect"
	"testing"
)

// Reference: pi v1.5.0 (f9a26365), api/bedrock-converse-stream.ts, and the
// assertions of test/bedrock-thinking-payload.test.ts ported verbatim (issues
// 10324 and 9331). Models come from the generated catalog, as upstream's
// getModel does, so a compat change is covered too. Only the fields the
// reference asserts are compared.
func bedrockCatalogModel(t *testing.T, id string) *Model {
	t.Helper()
	for _, model := range AmazonBedrockProvider().GetModels() {
		if model.ID == id {
			return model
		}
	}
	t.Fatalf("catalog has no %q", id)
	return nil
}

func bedrockThinking(t *testing.T, fields map[string]any) map[string]any {
	t.Helper()
	thinking, _ := fields["thinking"].(map[string]any)
	return thinking
}

func bedrockBeta(t *testing.T, fields map[string]any) []any {
	t.Helper()
	beta, _ := fields["anthropic_beta"].([]any)
	return beta
}

func TestBedrockAdaptiveThinkingBindingMatchesReference(t *testing.T) {
	binding := map[string]any{"prefix_mismatch_behavior": "drop_block"}
	adaptiveSummarized := map[string]any{"type": "adaptive", "display": "summarized"}

	// Claude Opus 5.5 accepts the binding.
	opus55 := bedrockCatalogModel(t, "global.anthropic.claude-opus-5-5")
	fields := BuildBedrockAdditionalModelRequestFields(opus55, &BedrockOptions{Reasoning: ThinkHigh})
	want := map[string]any{"type": "adaptive", "display": "summarized", "block_binding": binding}
	if got := bedrockThinking(t, fields); !reflect.DeepEqual(got, want) {
		t.Fatalf("5-5 thinking = %#v, want %#v", got, want)
	}
	if beta := bedrockBeta(t, fields); len(beta) != 1 || beta[0] != ThinkingBindingControlsBeta {
		t.Fatalf("5-5 beta = %#v, want [%s]", fields["anthropic_beta"], ThinkingBindingControlsBeta)
	}

	// Opus 4.6 and Sonnet 4.6 reject block_binding ("Extra inputs are not permitted").
	for _, id := range []string{"global.anthropic.claude-opus-4-6-v1", "global.anthropic.claude-sonnet-4-6"} {
		fields := BuildBedrockAdditionalModelRequestFields(bedrockCatalogModel(t, id), &BedrockOptions{Reasoning: ThinkHigh})
		if got := bedrockThinking(t, fields); !reflect.DeepEqual(got, adaptiveSummarized) {
			t.Errorf("%s thinking = %#v, want %#v", id, got, adaptiveSummarized)
		}
		if fields["anthropic_beta"] != nil {
			t.Errorf("%s beta = %#v, want none", id, fields["anthropic_beta"])
		}
	}
}

func TestBedrockGovCloudThinkingMatchesReference(t *testing.T) {
	// A GovCloud inference-profile id omits display on non-adaptive thinking.
	govSonnet := *bedrockCatalogModel(t, "us.anthropic.claude-sonnet-4-5-20250929-v1:0")
	govSonnet.ID = "us-gov.anthropic.claude-sonnet-4-5-20250929-v1:0"
	fields := BuildBedrockAdditionalModelRequestFields(&govSonnet, &BedrockOptions{Reasoning: ThinkHigh})
	want := map[string]any{"type": "enabled", "budget_tokens": 16384}
	if got := bedrockThinking(t, fields); !reflect.DeepEqual(got, want) {
		t.Fatalf("GovCloud 4-5 thinking = %#v, want %#v", got, want)
	}
	if beta := bedrockBeta(t, fields); len(beta) != 1 || beta[0] != "interleaved-thinking-2025-05-14" {
		t.Fatalf("GovCloud 4-5 beta = %#v", fields["anthropic_beta"])
	}

	// A GovCloud region omits display and the binding on adaptive thinking.
	globalOpus := *bedrockCatalogModel(t, "global.anthropic.claude-opus-4-6-v1")
	globalOpus.ID = "global.anthropic.claude-opus-4-8-v1"
	fields = BuildBedrockAdditionalModelRequestFields(&globalOpus, &BedrockOptions{Reasoning: ThinkHigh, Region: "us-gov-west-1"})
	if got := bedrockThinking(t, fields); !reflect.DeepEqual(got, map[string]any{"type": "adaptive"}) {
		t.Fatalf("GovCloud adaptive thinking = %#v", got)
	}
	config, _ := fields["output_config"].(map[string]any)
	if config["effort"] != "high" {
		t.Fatalf("GovCloud adaptive effort = %#v, want high", fields["output_config"])
	}
	if fields["anthropic_beta"] != nil {
		t.Fatalf("GovCloud adaptive beta = %#v, want none", fields["anthropic_beta"])
	}
}

func TestBedrockOpenAIReasoningEffortMatchesReference(t *testing.T) {
	// The configured thinking level must reach OpenAI models on Bedrock (#9331);
	// minimal maps to low.
	levels := []struct {
		level ThinkingLevel
		want  string
	}{
		{ThinkMinimal, "low"}, {ThinkLow, "low"}, {ThinkMedium, "medium"},
		{ThinkHigh, "high"}, {ThinkXHigh, "xhigh"}, {ThinkMax, "max"},
	}
	ids := []string{"global.openai.gpt-6-sol", "us.openai.gpt-6-luna", "global.openai.gpt-5.6-sol"}
	for _, level := range levels {
		for _, id := range ids {
			fields := BuildBedrockAdditionalModelRequestFields(bedrockCatalogModel(t, id), &BedrockOptions{Reasoning: level.level})
			want := map[string]any{"reasoning": map[string]any{"effort": level.want}}
			if !reflect.DeepEqual(fields, want) {
				t.Errorf("%s %s = %#v, want %#v", id, level.level, fields, want)
			}
		}
	}

	// An inference-profile ARN is still recognized by the model name.
	nameOnly := *bedrockCatalogModel(t, "global.openai.gpt-6-sol")
	nameOnly.ID = "arn:aws:bedrock:us-east-1:123456789012:application-inference-profile/my-profile"
	nameOnly.Name = "GPT-6 Sol"
	fields := BuildBedrockAdditionalModelRequestFields(&nameOnly, &BedrockOptions{Reasoning: ThinkMedium})
	if want := map[string]any{"reasoning": map[string]any{"effort": "medium"}}; !reflect.DeepEqual(fields, want) {
		t.Fatalf("name-only profile = %#v, want %#v", fields, want)
	}
}
