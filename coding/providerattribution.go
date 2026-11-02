package coding

import (
	"context"
	"os"
	"strings"
	"sync"

	"github.com/dat267/pier/ai"
)

// Port of core/client-identity.ts. Attribution and session routing live in ai;
// this file resolves the coding identity that request options carry.

// ClientIdentity resolves how session requests identify pi: a detailed user
// agent with the build version, attributing requests to pi while install
// telemetry is enabled (upstream getClientIdentity, v1.4.0 adc5fa9f8). A nil
// attribution sends no attribution headers.
func ClientIdentity(settingsManager *SettingsManager) *ai.ClientIdentity {
	identity := &ai.ClientIdentity{UserAgent: "detailed", Version: Version, VersionSet: true, AttributionSet: true}
	if IsInstallTelemetryEnabled(settingsManager) {
		identity.Attribution = ai.PIAttribution
	}
	return identity
}

// IsInstallTelemetryEnabled reports whether install telemetry is on: the
// PI_TELEMETRY environment variable wins over the settings toggle.
func IsInstallTelemetryEnabled(settingsManager *SettingsManager) bool {
	return IsInstallTelemetryEnabledWithEnv(settingsManager, telemetryEnvValue())
}

// IsInstallTelemetryEnabledWithEnv is IsInstallTelemetryEnabled with an
// explicit PI_TELEMETRY value; empty means unset.
func IsInstallTelemetryEnabledWithEnv(settingsManager *SettingsManager, telemetryEnv string) bool {
	if telemetryEnv != "" {
		return IsTruthyEnvFlag(telemetryEnv)
	}
	if settingsManager == nil {
		return true
	}
	return settingsManager.GetEnableInstallTelemetry()
}

// RuntimeCredentials overlays non-persistent runtime API keys on a credential
// store (port of core/runtime-credentials.ts).
//
// ModelRuntime serializes writes per provider, but reads (GetAuth,
// GetAvailable) run on turn goroutines, so the override map needs its own lock
// (M69). Store calls happen outside it: the store has its own admission.
type RuntimeCredentials struct {
	store     ai.CredentialStore
	mu        sync.RWMutex
	overrides map[string]string
}

// NewRuntimeCredentials wraps a credential store.
func NewRuntimeCredentials(store ai.CredentialStore) *RuntimeCredentials {
	return &RuntimeCredentials{store: store, overrides: map[string]string{}}
}

// SetRuntimeAPIKey records a runtime API key for a provider.
func (r *RuntimeCredentials) SetRuntimeAPIKey(providerID, apiKey string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.overrides[providerID] = apiKey
}

// RemoveRuntimeAPIKey drops a runtime API key for a provider.
func (r *RuntimeCredentials) RemoveRuntimeAPIKey(providerID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.overrides, providerID)
}

// HasRuntimeAPIKey reports whether a runtime API key is set.
func (r *RuntimeCredentials) HasRuntimeAPIKey(providerID string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.overrides[providerID]
	return ok
}

// Read returns the runtime override, otherwise the stored credential.
func (r *RuntimeCredentials) Read(providerID string, ctx context.Context) (*ai.Credential, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r.mu.RLock()
	override, ok := r.overrides[providerID]
	r.mu.RUnlock()
	if ok {
		return &ai.Credential{Type: ai.CredentialAPIKey, APIKey: &ai.ApiKeyCredential{Key: override}}, nil
	}
	return r.store.Read(providerID, ctx)
}

// List returns stored credential metadata plus the runtime overrides.
func (r *RuntimeCredentials) List(ctx context.Context) ([]ai.CredentialInfo, error) {
	entries, err := r.store.List(ctx)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r.mu.RLock()
	overrides := make(map[string]string, len(r.overrides))
	for providerID, key := range r.overrides {
		overrides[providerID] = key
	}
	r.mu.RUnlock()

	byProvider := map[string]ai.CredentialInfo{}
	var order []string
	for _, entry := range entries {
		if _, seen := byProvider[entry.ProviderID]; !seen {
			order = append(order, entry.ProviderID)
		}
		byProvider[entry.ProviderID] = entry
	}
	for providerID := range overrides {
		if _, seen := byProvider[providerID]; !seen {
			order = append(order, providerID)
		}
		byProvider[providerID] = ai.CredentialInfo{ProviderID: providerID, Type: ai.CredentialAPIKey}
	}
	out := make([]ai.CredentialInfo, 0, len(order))
	for _, providerID := range order {
		out = append(out, byProvider[providerID])
	}
	return out, nil
}

// Modify delegates to the wrapped store.
func (r *RuntimeCredentials) Modify(providerID string, fn func(current *ai.Credential) (*ai.Credential, error), ctx context.Context) (*ai.Credential, error) {
	return r.store.Modify(providerID, fn, ctx)
}

// Delete removes the stored credential and any runtime override.
func (r *RuntimeCredentials) Delete(providerID string, ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	err := r.store.Delete(providerID, ctx)
	if err != nil {
		return err
	}
	r.mu.Lock()
	delete(r.overrides, providerID)
	r.mu.Unlock()
	return nil
}

// IsTruthyEnvFlag mirrors the upstream env flag check: "1", "true", or "yes"
// (case-insensitive).
func IsTruthyEnvFlag(value string) bool {
	if value == "" {
		return false
	}
	if value == "1" {
		return true
	}
	lower := strings.ToLower(value)
	return lower == "true" || lower == "yes"
}

// telemetryEnvValue reads PI_TELEMETRY.
func telemetryEnvValue() string {
	return os.Getenv("PI_TELEMETRY")
}
