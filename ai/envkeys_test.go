package ai

import "testing"

// Each Qwen Token Plan checks its own variable first, then the region variable
// both plans used to share, so a team key and an individual key can coexist
// (env-api-keys.ts, v1.6.0, #12112).
func TestQwenTokenPlanEnvVarsPerPlan(t *testing.T) {
	cases := map[string][]string{
		"qwen-token-plan":               {"QWEN_TOKEN_PLAN_TEAM_API_KEY", "QWEN_TOKEN_PLAN_API_KEY"},
		"qwen-token-plan-cn":            {"QWEN_TOKEN_PLAN_TEAM_CN_API_KEY", "QWEN_TOKEN_PLAN_CN_API_KEY"},
		"qwen-token-plan-individual":    {"QWEN_TOKEN_PLAN_INDIVIDUAL_API_KEY", "QWEN_TOKEN_PLAN_API_KEY"},
		"qwen-token-plan-individual-cn": {"QWEN_TOKEN_PLAN_INDIVIDUAL_CN_API_KEY", "QWEN_TOKEN_PLAN_CN_API_KEY"},
	}
	for provider, want := range cases {
		got := apiKeyEnvVars(provider)
		if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
			t.Errorf("%s: %v, want %v", provider, got, want)
		}
	}
	// The individual plan's own variable wins over the shared one.
	env := ProviderEnv{
		"QWEN_TOKEN_PLAN_INDIVIDUAL_API_KEY": "individual",
		"QWEN_TOKEN_PLAN_API_KEY":            "shared",
	}
	if got := GetEnvApiKey("qwen-token-plan-individual", env); got != "individual" {
		t.Fatalf("api key = %q, want the individual plan's variable", got)
	}
}
