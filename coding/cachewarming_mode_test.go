package coding

import (
	"testing"

	"github.com/dat267/pier/ai"
)

// SetCacheWarmingMode reconciles whatever the session has: a session without a
// settings manager or a warmer still accepts the call (the guards are nil
// checks, and inverting them calls a method on a nil value).
func TestSetCacheWarmingModeWithoutSettingsOrWarmer(t *testing.T) {
	tempAgentDir(t)
	manager := NewSessionManager(t.TempDir(), nil)
	session, err := NewAgentSession(&SessionConfig{
		Cwd:      t.TempDir(),
		Sessions: manager,
		Model:    &ai.Model{ID: "m", API: ai.APIAnthropicMessages, Provider: "anthropic", ContextWindow: 1000},
		StreamFn: stubStreamFn,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Dispose()
	session.SetCacheWarmingMode(CacheWarmingStreaming)

	settings := NewInMemorySettingsManager(&Settings{}, SettingsManagerCreateOptions{})
	session.control.Settings = settings
	session.SetCacheWarmingMode(CacheWarmingOff)
	if got := settings.GetCacheWarmingMode(); got != CacheWarmingOff {
		t.Fatalf("cache warming mode = %q, want off", got)
	}
}
