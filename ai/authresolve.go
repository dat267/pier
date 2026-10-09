package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Port of auth/resolve.ts: auth resolution shared by the Models collection.

// ModelsErrorCode enumerates ModelsError codes.
type ModelsErrorCode = string

const (
	ErrCodeProvider ModelsErrorCode = "provider"
	ErrCodeStream   ModelsErrorCode = "stream"
	ErrCodeAuth     ModelsErrorCode = "auth"
	ErrCodeOAuth    ModelsErrorCode = "oauth"
)

// ModelsError is the error type surfaced by the Models collection.
type ModelsError struct {
	Code    ModelsErrorCode
	Message string
	Cause   error
}

func (e *ModelsError) Error() string { return e.Message }
func (e *ModelsError) Unwrap() error { return e.Cause }

// NewModelsError builds a ModelsError, folding the cause's message into the
// error message (upstream withCauseDetail: callers surface error.message
// only, so the underlying reason is kept in it).
func NewModelsError(code ModelsErrorCode, message string, cause error) *ModelsError {
	if cause != nil {
		detail := strings.TrimSpace(cause.Error())
		if detail != "" && !strings.Contains(message, detail) {
			message = message + ": " + detail
		}
	}
	return &ModelsError{Code: code, Message: message, Cause: cause}
}

// AuthResolutionOverrides tunes auth resolution.
type AuthResolutionOverrides struct {
	APIKey string
	Env    ProviderEnv
	// MinOAuthValidityMS requires this much remaining OAuth-token validity;
	// defaults to five minutes.
	MinOAuthValidityMS int64
	Ctx                context.Context // upstream signal
}

const (
	oauthMinimumValidityMS = 5 * 60 * 1000
	oauthRefreshTimeoutMS  = 15 * 1000
)

// ResolveProviderAuth resolves auth for a provider. A stored credential owns
// the provider: ambient/env is consulted only when nothing is stored. No
// silent env fallback after a failed refresh or for a credential type
// without a matching handler.
func ResolveProviderAuth(providerID string, auth ProviderAuth, credentials CredentialStore, authContext AuthContext, overrides *AuthResolutionOverrides) (*AuthResult, error) {
	ctx := context.Background()
	if overrides != nil && overrides.Ctx != nil {
		ctx = overrides.Ctx
	}
	if err := ctxErr(ctx); err != nil {
		return nil, err
	}
	requestAuthContext := authContext
	if overrides != nil && overrides.Env != nil {
		requestAuthContext = overlayEnvAuthContext(authContext, overrides.Env)
	}

	if overrides != nil && overrides.APIKey != "" && auth.APIKey != nil {
		return resolveAPIKey(requestAuthContext, auth.APIKey, providerID, &ApiKeyCredential{
			Key: overrides.APIKey,
			Env: overrides.Env,
		}, ctx)
	}

	stored, err := readCredential(credentials, providerID, ctx)
	if err != nil {
		return nil, err
	}
	if stored != nil {
		if stored.Type == CredentialOAuth && auth.OAuth != nil {
			return resolveStoredOAuth(credentials, providerID, auth.OAuth, stored.OAuth, ctx, overrides.minValidity())
		}
		if stored.Type == CredentialAPIKey && auth.APIKey != nil {
			credential := stored.APIKey
			if overrides != nil && overrides.Env != nil {
				merged := ProviderEnv{}
				for k, v := range credential.Env {
					merged[k] = v
				}
				for k, v := range overrides.Env {
					merged[k] = v
				}
				credential = &ApiKeyCredential{Key: credential.Key, Env: merged}
			}
			return resolveAPIKey(requestAuthContext, auth.APIKey, providerID, credential, ctx)
		}
		return nil, nil
	}

	// Ambient (env vars, AWS profiles, ADC files).
	if auth.APIKey != nil {
		return resolveAPIKey(requestAuthContext, auth.APIKey, providerID, nil, ctx)
	}
	return nil, nil
}

func (o *AuthResolutionOverrides) minValidity() int64 {
	if o == nil || o.MinOAuthValidityMS == 0 {
		return 0
	}
	return o.MinOAuthValidityMS
}

func overlayEnvAuthContext(base AuthContext, env ProviderEnv) AuthContext {
	return &overlayEnvContext{base: base, env: env}
}

type overlayEnvContext struct {
	base AuthContext
	env  ProviderEnv
}

func (o *overlayEnvContext) Env(name string) (string, bool) {
	if v, ok := o.env[name]; ok && v != "" {
		return v, true
	}
	return o.base.Env(name)
}

func (o *overlayEnvContext) FileExists(path string) bool { return o.base.FileExists(path) }

// refreshStoredOAuthCredential refreshes a stored OAuth credential under the
// credential-store lock and persists the result before the lock is released.
// needsRefresh is re-checked under the lock, so concurrent callers and
// processes refresh only once.
//
// ctx cancels only the wait for the credential lock. Once a refresh starts the
// provider may already have rotated the refresh token, so the refresh and its
// persistence ignore ctx and are bounded only by oauthRefreshTimeoutMS.
// Otherwise a cancelled caller could discard the only valid refresh token
// (upstream bde882c74). Returns nil when the provider no longer has an OAuth
// credential.
func refreshStoredOAuthCredential(credentials CredentialStore, providerID string, oauth *OAuthAuth, needsRefresh func(*OAuthCredential) bool, ctx context.Context) (*OAuthCredential, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	// The caller's cancellation stops the wait for the lock, then detaches
	// once the callback runs so the rotated credential still lands.
	lockWaitCtx, cancelLockWait := context.WithCancel(context.Background())
	stopLockWait := context.AfterFunc(ctx, cancelLockWait)
	defer func() {
		stopLockWait()
		cancelLockWait()
	}()

	post, err := credentials.Modify(providerID, func(current *Credential) (*Credential, error) {
		// From here the refresh must complete: the provider may already have
		// rotated the refresh token.
		stopLockWait()
		if err := ctxErr(ctx); err != nil {
			return nil, err
		}
		if current == nil || current.Type != CredentialOAuth {
			return nil, nil // logged out meanwhile
		}
		if !needsRefresh(current.OAuth) {
			return nil, nil // another process/request refreshed
		}
		refreshCtx, cancel := context.WithTimeout(context.Background(), oauthRefreshTimeoutMS*time.Millisecond)
		defer cancel()
		refreshed, err := oauth.Refresh(current.OAuth, refreshCtx)
		if err != nil {
			return nil, NewModelsError(ErrCodeOAuth, fmt.Sprintf("OAuth refresh failed for %s", providerID), err)
		}
		return &Credential{Type: CredentialOAuth, OAuth: refreshed}, nil
	}, lockWaitCtx)
	if err != nil {
		var me *ModelsError
		if asModelsError(err, &me) {
			return nil, err
		}
		if err := ctxErr(ctx); err != nil {
			return nil, err
		}
		return nil, NewModelsError(ErrCodeAuth, fmt.Sprintf("Credential store modify failed for %s", providerID), err)
	}
	if post == nil || post.Type != CredentialOAuth {
		return nil, nil // logged out meanwhile
	}
	return post.OAuth, nil
}

// resolveStoredOAuth implements OAuth resolution with double-checked
// locking: tokens with less than five minutes remaining lock, re-check
// expiry under the lock, refresh once globally, and persist the rotated
// credential before release.
func resolveStoredOAuth(credentials CredentialStore, providerID string, oauth *OAuthAuth, stored *OAuthCredential, ctx context.Context, minOAuthValidityMS int64) (*AuthResult, error) {
	minimumValidityMS := int64(oauthMinimumValidityMS)
	if minOAuthValidityMS > minimumValidityMS {
		minimumValidityMS = minOAuthValidityMS
	}
	expiresSoon := func(credential *OAuthCredential) bool {
		return time.Now().UnixMilli()+minimumValidityMS >= credential.Expires
	}
	credential := stored

	if expiresSoon(credential) {
		// Optimistic check said expired; the authoritative check runs under the lock.
		post, err := refreshStoredOAuthCredential(credentials, providerID, oauth, func(current *OAuthCredential) bool {
			return expiresSoon(current)
		}, ctx)
		if err != nil {
			return nil, err
		}
		if post == nil {
			return nil, nil // logged out meanwhile
		}
		// The rotated credential is persisted even if the caller cancelled
		// during the refresh; the caller still observes the abort, matching
		// upstream's raceWithAbortSignal.
		if err := ctxErr(ctx); err != nil {
			return nil, err
		}
		credential = post
		// The normal five-minute window triggers a refresh but does not impose
		// a provider contract. Explicit callers (such as bearer-token export)
		// do require the requested minimum after the refresh.
		if minOAuthValidityMS != 0 && expiresSoon(credential) {
			return nil, NewModelsError(ErrCodeOAuth, fmt.Sprintf("OAuth refresh returned a token that expires too soon for %s", providerID), nil)
		}
	}

	auth, err := oauth.ToAuth(credential)
	if err != nil {
		return nil, NewModelsError(ErrCodeOAuth, fmt.Sprintf("OAuth auth derivation failed for %s", providerID), err)
	}
	return &AuthResult{Auth: *auth, Source: "OAuth"}, nil
}

func asModelsError(err error, target **ModelsError) bool {
	if me, ok := err.(*ModelsError); ok {
		*target = me
		return true
	}
	return false
}

func resolveAPIKey(authContext AuthContext, apiKey *ApiKeyAuth, providerID string, credential *ApiKeyCredential, ctx context.Context) (*AuthResult, error) {
	// D38: upstream's ApiKeyAuth.resolve is required, so a missing one is a
	// programmer error there. Go function fields cannot be required, so a nil
	// Resolve means "this provider has no api-key resolution" and reports no
	// auth instead of panicking.
	if apiKey.Resolve == nil {
		return nil, nil
	}
	result, err := apiKey.Resolve(AuthResolveInput{Ctx: authContext, Credential: credential, Ctx2: ctx})
	if err != nil {
		return nil, NewModelsError(ErrCodeAuth, fmt.Sprintf("API key auth failed for provider %s", providerID), err)
	}
	return result, nil
}

func readCredential(credentials CredentialStore, providerID string, ctx context.Context) (*Credential, error) {
	credential, err := credentials.Read(providerID, ctx)
	if err != nil {
		return nil, NewModelsError(ErrCodeAuth, fmt.Sprintf("Credential store read failed for %s", providerID), err)
	}
	return credential, nil
}

// jsonUnmarshalStrict decodes JSON without HTML-escape concerns (decode path).
func jsonUnmarshalStrict(data []byte, v any) error {
	return json.Unmarshal(data, v)
}
