package ai

import (
	"context"
	"os"
)

// Port of utils/provider-env.ts. The Bun sandbox fallback (/proc/self/environ)
// is Node-runtime specific and does not apply to Go; scoped overrides then
// os.Getenv covers the same resolution order otherwise.

// GetProviderEnvValue resolves a provider env value from scoped overrides,
// then the process environment (upstream order: env?.[name] || process.env[name]).
// Blank values are falsy (JS semantics).
func GetProviderEnvValue(name string, env ProviderEnv) (string, bool) {
	if v, ok := env[name]; ok && v != "" {
		return v, true
	}
	if v := os.Getenv(name); v != "" {
		return v, true
	}
	return "", false
}

// GetProviderEnvValueOr is the undefined-yielding form used where upstream
// relies on `|| undefined`.
func GetProviderEnvValueOr(name string, env ProviderEnv) string {
	v, _ := GetProviderEnvValue(name, env)
	return v
}

var _ = context.Background
