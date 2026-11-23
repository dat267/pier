package coding

import (
	ctxpkg "context"
	"github.com/dat267/pier/ai"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

// Round 120 tests: the CreateAgentSession assembly (model restore/fallback,
// thinking-level restore and clamp, tool selection, the block-images filter,
// and the settings-backed request options).

func TestCreateAgentSessionBlockImages(t *testing.T) {
	tempAgentDir(t)
	runtime, err := CreateModelRuntime(CreateModelRuntimeOptions{
		AuthPath:        filepath.Join(GetAgentDir(), "auth.json"),
		ModelsPath:      filepath.Join(GetAgentDir(), "models.json"),
		Credentials:     newMemoryCredentialStore(),
		RefreshOnCreate: new(false),
	})
	if err != nil {
		t.Fatal(err)
	}
	settings := NewSettingsManagerFromFiles(t.TempDir(), t.TempDir(), SettingsManagerCreateOptions{})
	session, err := CreateAgentSession(ctxpkg.Background(), &CreateAgentSessionOptions{
		Cwd: t.TempDir(), ModelRuntime: runtime, SettingsManager: settings,
		Model: &ai.Model{ID: "m", API: ai.APIAnthropicMessages, Provider: "anthropic", ContextWindow: 1000, MaxTokens: 100},
	})
	if err != nil {
		t.Fatal(err)
	}

	// Block-images filters the transcript on conversion.
	settings.SetBlockImages(true)
	messages := []ai.Message{
		&ai.UserMessage{Content: ai.StringOrBlocks{Blocks: ai.ContentList{
			ai.ImageContent{Data: "AAAA", MimeType: "image/png"},
			ai.ImageContent{Data: "BBBB", MimeType: "image/png"},
			ai.TextContent{Text: "keep"},
		}}},
		&ai.ToolResultMessage{ToolCallID: "t", ToolName: "read", Content: ai.UserContentList{
			ai.ImageContent{Data: "CCCC", MimeType: "image/png"},
		}},
	}
	converted := session.Session.Agent.ConvertToLlm(messages)
	user := converted[0].(*ai.UserMessage)
	if len(user.Content.Blocks) != 2 {
		t.Fatalf("blocks = %+v", user.Content.Blocks)
	}
	if text, ok := user.Content.Blocks[0].(ai.TextContent); !ok || text.Text != "Image reading is disabled." {
		t.Fatalf("blocks = %+v", user.Content.Blocks)
	}
	if text, ok := user.Content.Blocks[1].(ai.TextContent); !ok || text.Text != "keep" {
		t.Fatalf("blocks = %+v", user.Content.Blocks)
	}
	toolResult := converted[1].(*ai.ToolResultMessage)
	if len(toolResult.Content) != 1 {
		t.Fatalf("tool result = %+v", toolResult.Content)
	}
	if text, ok := toolResult.Content[0].(ai.TextContent); !ok || text.Text != "Image reading is disabled." {
		t.Fatalf("tool result = %+v", toolResult.Content)
	}

	// The setting is read per conversion: disabling restores the images.
	settings.SetBlockImages(false)
	converted = session.Session.Agent.ConvertToLlm(messages)
	user = converted[0].(*ai.UserMessage)
	if len(user.Content.Blocks) != 3 {
		t.Fatalf("blocks = %+v", user.Content.Blocks)
	}
}

// Upstream (pi v1.5.2 core/sdk.ts convertToLlmWithBlockImages) replaces only the
// content of a filtered message (`{ ...msg, content }`), so every other field
// survives the block-images pass.
func TestCreateAgentSessionBlockImagesKeepsMessageFields(t *testing.T) {
	tempAgentDir(t)
	runtime := testSessionRuntime(t)
	settings := NewSettingsManagerFromFiles(t.TempDir(), t.TempDir(), SettingsManagerCreateOptions{})
	session, err := CreateAgentSession(ctxpkg.Background(), &CreateAgentSessionOptions{
		Cwd: t.TempDir(), ModelRuntime: runtime, SettingsManager: settings,
		Model: &ai.Model{ID: "m", API: ai.APIAnthropicMessages, Provider: "anthropic", ContextWindow: 1000, MaxTokens: 100},
	})
	if err != nil {
		t.Fatal(err)
	}
	settings.SetBlockImages(true)
	duration := int64(42)
	nested := &ai.NestedToolCalls{Complete: true}
	details := []byte(`{"nested":true}`)
	messages := []ai.Message{
		&ai.UserMessage{Content: ai.StringOrBlocks{Blocks: ai.ContentList{
			ai.ImageContent{Data: "AAAA", MimeType: "image/png"},
		}}, Timestamp: 7},
		&ai.ToolResultMessage{
			ToolCallID: "t", ToolName: "read", Content: ai.UserContentList{
				ai.ImageContent{Data: "CCCC", MimeType: "image/png"},
			},
			Details: details, Usage: &ai.Usage{Input: 3}, NestedCalls: nested,
			IsError: true, DurationMs: &duration, Timestamp: 9,
		},
	}
	converted := session.Session.Agent.ConvertToLlm(messages)
	user := converted[0].(*ai.UserMessage)
	if user.Timestamp != 7 {
		t.Fatalf("user timestamp = %d, want 7", user.Timestamp)
	}
	toolResult := converted[1].(*ai.ToolResultMessage)
	if toolResult.ToolCallID != "t" || toolResult.ToolName != "read" || !toolResult.IsError {
		t.Fatalf("tool result identity = %+v", toolResult)
	}
	if toolResult.Timestamp != 9 {
		t.Fatalf("timestamp = %d, want 9", toolResult.Timestamp)
	}
	if toolResult.DurationMs == nil || *toolResult.DurationMs != duration {
		t.Fatalf("duration = %v, want %d", toolResult.DurationMs, duration)
	}
	if toolResult.NestedCalls != nested {
		t.Fatalf("nested calls = %+v, want the original record", toolResult.NestedCalls)
	}
	if string(toolResult.Details) != string(details) {
		t.Fatalf("details = %s", toolResult.Details)
	}
	if toolResult.Usage == nil || toolResult.Usage.Input != 3 {
		t.Fatalf("usage = %+v", toolResult.Usage)
	}
}

func TestCreateAgentSessionRequestOptions(t *testing.T) {
	// A set PI_TELEMETRY wins over the settings toggle, so unset it to exercise
	// the toggle the second prompt flips (upstream core/telemetry.ts).
	previousTelemetry, hadTelemetry := os.LookupEnv("PI_TELEMETRY")
	if err := os.Unsetenv("PI_TELEMETRY"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if hadTelemetry {
			_ = os.Setenv("PI_TELEMETRY", previousTelemetry)
		}
	})
	tempAgentDir(t)
	runtime, err := CreateModelRuntime(CreateModelRuntimeOptions{
		AuthPath:        filepath.Join(GetAgentDir(), "auth.json"),
		ModelsPath:      filepath.Join(GetAgentDir(), "models.json"),
		Credentials:     newMemoryCredentialStore(),
		RefreshOnCreate: new(false),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.SetRuntimeAPIKey(ctxpkg.Background(), "anthropic", "sk-1"); err != nil {
		t.Fatal(err)
	}
	settingsDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(settingsDir, ConfigDirName), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(settingsDir, ConfigDirName, "settings.json"), []byte(
		`{"retry":{"provider":{"timeoutMs":45000,"maxRetries":7}},"httpIdleTimeoutMs":"disabled","transport":"sse"}`),
		0o600); err != nil {
		t.Fatal(err)
	}
	settings := NewSettingsManagerFromFiles(settingsDir, agentDirForSettings(t), SettingsManagerCreateOptions{})

	var captured *ai.SimpleStreamOptions
	var prompts atomic.Int64
	streamFn := func(model *ai.Model, context ai.TranscriptContext, options *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
		prompts.Add(1)
		captured = options
		stream := ai.NewAssistantMessageEventStream()
		go func() {
			message := &ai.AssistantMessage{
				API: ai.APIAnthropicMessages, Provider: model.Provider, Model: model.ID,
				Content: ai.ContentList{ai.TextContent{Text: "ack"}}, StopReason: ai.StopStop,
			}
			stream.Push(ai.AssistantMessageEvent{Type: ai.EventDone, Reason: ai.StopStop, Message: message})
		}()
		return stream
	}
	session, err := CreateAgentSession(ctxpkg.Background(), &CreateAgentSessionOptions{
		Cwd: t.TempDir(), ModelRuntime: runtime, SettingsManager: settings, StreamFn: streamFn,
		Model:         runtime.GetModel("anthropic", "anthropic-model"),
		ThinkingLevel: ai.ThinkLow,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := session.Session.Prompt(ctxpkg.Background(), "hello", nil); err != nil {
		t.Fatal(err)
	}
	if prompts.Load() != 1 {
		t.Fatalf("prompts = %d", prompts.Load())
	}
	if captured == nil {
		t.Fatal("options missing")
	}
	// The settings-backed request options are applied.
	if captured.TimeoutMs == nil || *captured.TimeoutMs != 45000 {
		t.Fatalf("timeout = %v", captured.TimeoutMs)
	}
	if captured.HTTPIdleTimeoutMs == nil || *captured.HTTPIdleTimeoutMs != 0 {
		t.Fatalf("HTTP idle timeout = %v, want disabled independently of provider timeout", captured.HTTPIdleTimeoutMs)
	}
	if captured.MaxRetries == nil || *captured.MaxRetries != 7 {
		t.Fatalf("maxRetries = %v", captured.MaxRetries)
	}
	if captured.Transport != ai.TransportSSE {
		t.Fatalf("transport = %q", captured.Transport)
	}
	if captured.SessionID == "" {
		t.Fatal("session id missing")
	}
	// v1.4.0 core/client-identity.ts and core/sdk.ts: the identity carries
	// the build version and reads the telemetry setting on every request.
	if captured.Identity == nil || captured.Identity.Version != Version || captured.Identity.Attribution != ai.PIAttribution {
		t.Fatalf("identity = %+v", captured.Identity)
	}
	settings.SetEnableInstallTelemetry(false)
	if err := session.Session.Prompt(ctxpkg.Background(), "again", nil); err != nil {
		t.Fatal(err)
	}
	if captured.Identity == nil || captured.Identity.Version != Version || captured.Identity.Attribution != nil {
		t.Fatalf("telemetry-off identity = %+v", captured.Identity)
	}
	// No attribution headers are injected into caller headers. Provider
	// defaults now come from identity, so null caller overrides survive.
	if captured.Headers != nil {
		t.Fatalf("headers = %v", captured.Headers)
	}
	// The steering/follow-up modes come from settings.
	if session.Session.SteeringMode() != QueueModeOneAtATime {
		t.Fatalf("steering mode = %q", session.Session.SteeringMode())
	}
}

// The global HTTP idle timeout and provider SDK timeout are propagated
// separately, so provider retry overrides cannot replace the dispatcher's body
// idle limit. Per-request values are rebuilt after settings changes instead of
// mutating the shared transport (D254).
func TestHTTPIdleTimeoutReachesTheRequest(t *testing.T) {
	tempAgentDir(t)
	runtime, err := CreateModelRuntime(CreateModelRuntimeOptions{
		AuthPath:        filepath.Join(GetAgentDir(), "auth.json"),
		ModelsPath:      filepath.Join(GetAgentDir(), "models.json"),
		Credentials:     newMemoryCredentialStore(),
		RefreshOnCreate: new(false),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.SetRuntimeAPIKey(ctxpkg.Background(), "anthropic", "sk-1"); err != nil {
		t.Fatal(err)
	}
	model := &ai.Model{
		ID: "m", API: ai.APIAnthropicMessages, Provider: "anthropic",
		ContextWindow: 100000, MaxTokens: 8192,
	}

	var captured *ai.SimpleStreamOptions
	streamFn := func(streamModel *ai.Model, context ai.TranscriptContext, options *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
		captured = options
		stream := ai.NewAssistantMessageEventStream()
		go func() {
			message := &ai.AssistantMessage{
				API: ai.APIAnthropicMessages, Provider: streamModel.Provider, Model: streamModel.ID,
				Content: ai.ContentList{ai.TextContent{Text: "ack"}}, StopReason: ai.StopStop,
			}
			stream.Push(ai.AssistantMessageEvent{Type: ai.EventDone, Reason: ai.StopStop, Message: message})
		}()
		return stream
	}
	// requestTimeout builds a session with the given setting and returns the
	// timeout the provider request was built with. No provider retry timeout is
	// configured, so the idle timeout is what answers.
	requestTimeout := func(t *testing.T, settingsJSON string) int {
		t.Helper()
		settingsDir := t.TempDir()
		if err := os.MkdirAll(filepath.Join(settingsDir, ConfigDirName), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(settingsDir, ConfigDirName, "settings.json"), []byte(settingsJSON), 0o600); err != nil {
			t.Fatal(err)
		}
		settings := NewSettingsManagerFromFiles(settingsDir, agentDirForSettings(t), SettingsManagerCreateOptions{})
		captured = nil
		session, err := CreateAgentSession(ctxpkg.Background(), &CreateAgentSessionOptions{
			Cwd: t.TempDir(), ModelRuntime: runtime, SettingsManager: settings, StreamFn: streamFn, Model: model,
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := session.Session.Prompt(ctxpkg.Background(), "hello", nil); err != nil {
			t.Fatal(err)
		}
		if captured == nil || captured.TimeoutMs == nil {
			t.Fatalf("no request timeout captured for %s", settingsJSON)
		}
		return *captured.TimeoutMs
	}

	if got := requestTimeout(t, `{"httpIdleTimeoutMs":60000}`); got != 60_000 {
		t.Fatalf("timeout = %d, want the configured 60000", got)
	}
	if captured.HTTPIdleTimeoutMs == nil || *captured.HTTPIdleTimeoutMs != 60_000 {
		t.Fatalf("HTTP idle timeout = %v, want 60000", captured.HTTPIdleTimeoutMs)
	}
	// "disabled" is the max-int32 sentinel for the SDK timeout, while the
	// independent dispatcher body-idle timeout is explicitly disabled.
	if got := requestTimeout(t, `{"httpIdleTimeoutMs":"disabled"}`); got != 2147483647 {
		t.Fatalf("disabled timeout = %d, want the no-timeout sentinel", got)
	}
	if captured.HTTPIdleTimeoutMs == nil || *captured.HTTPIdleTimeoutMs != 0 {
		t.Fatalf("disabled HTTP idle timeout = %v, want 0", captured.HTTPIdleTimeoutMs)
	}
}

func TestCreateAgentSessionCacheWarmerStarts(t *testing.T) {
	tempAgentDir(t)
	runtime, err := CreateModelRuntime(CreateModelRuntimeOptions{
		AuthPath:        filepath.Join(GetAgentDir(), "auth.json"),
		ModelsPath:      filepath.Join(GetAgentDir(), "models.json"),
		Credentials:     newMemoryCredentialStore(),
		RefreshOnCreate: new(false),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.SetRuntimeAPIKey(ctxpkg.Background(), "anthropic", "sk-1"); err != nil {
		t.Fatal(err)
	}
	settings := NewSettingsManagerFromFiles(t.TempDir(), t.TempDir(), SettingsManagerCreateOptions{})
	settings.SetCacheWarmingMode(CacheWarmingIdle)

	model := &ai.Model{
		ID: "m", API: ai.APIAnthropicMessages, Provider: "anthropic",
		ContextWindow: 100000, MaxTokens: 8192,
		PromptCache: ai.ModelPromptCache{ai.CacheRetentionShort: 11},
	}
	var prompts atomic.Int64
	streamFn := func(streamModel *ai.Model, context ai.TranscriptContext, options *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
		prompts.Add(1)
		stream := ai.NewAssistantMessageEventStream()
		go func() {
			message := &ai.AssistantMessage{
				API: ai.APIAnthropicMessages, Provider: streamModel.Provider, Model: streamModel.ID,
				Content: ai.ContentList{ai.TextContent{Text: "ack"}}, StopReason: ai.StopStop,
				Usage: ai.Usage{Input: 1000, TotalTokens: 1000},
			}
			stream.Push(ai.AssistantMessageEvent{Type: ai.EventDone, Reason: ai.StopStop, Message: message})
		}()
		return stream
	}
	session, err := CreateAgentSession(ctxpkg.Background(), &CreateAgentSessionOptions{
		Cwd: t.TempDir(), ModelRuntime: runtime, SettingsManager: settings, StreamFn: streamFn, Model: model,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := session.Session.Prompt(ctxpkg.Background(), "hello", nil); err != nil {
		t.Fatal(err)
	}
	// The warmer is scheduled from the session request (the session id matches).
	status := session.Session.GetCacheWarmingStatus()
	if status == nil {
		t.Fatal("cache warming status missing")
	}
	if status.State != "scheduled" && status.State != "refreshing" && status.State != "inactive" {
		t.Fatalf("status = %+v", status)
	}
	// The warm run is scheduled from the session request; the economics make pi
	// stop before spending money, so no warm request fires here.
	if prompts.Load() < 1 {
		t.Fatalf("session prompt did not run: %d", prompts.Load())
	}
}

// Upstream core/sdk.ts assembles the built-in tools without a filesystem
// sandbox. Confinement belongs to extensions, not the session system prompt.
func TestCreateAgentSessionDoesNotInjectSandboxPolicy(t *testing.T) {
	tempAgentDir(t)
	runtime, err := CreateModelRuntime(CreateModelRuntimeOptions{
		Credentials: newMemoryCredentialStore(), RefreshOnCreate: new(false),
	})
	if err != nil {
		t.Fatal(err)
	}
	cwd := t.TempDir()
	result, err := CreateAgentSession(ctxpkg.Background(), &CreateAgentSessionOptions{
		Cwd: cwd, ModelRuntime: runtime,
		SessionManager:  NewSessionManager(cwd, nil),
		SettingsManager: NewSettingsManagerFromFiles(cwd, filepath.Join(t.TempDir(), "agent"), SettingsManagerCreateOptions{}),
	})
	if err != nil {
		t.Fatal(err)
	}
	prompt := result.Session.SystemPrompt()
	if strings.Contains(prompt, "<sandbox>") {
		t.Fatal("built-in session injected a sandbox policy absent from pi")
	}
}
