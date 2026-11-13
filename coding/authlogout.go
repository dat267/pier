package coding

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"

	"github.com/dat267/pier/ai"
	"github.com/dat267/pier/tui"
)

// RunAuthLogout implements `pi auth logout <provider>` (pi v1.5.2
// cli/auth-logout.ts): it removes the credential a login stored and says which
// kind it removed. Environment variables and models.json config stay, as with
// /logout.
func runAuthLogout(appName string, command *AuthCommand, stdout, stderr io.Writer) int {
	providerRef := ""
	if len(command.Args) > 0 {
		providerRef = command.Args[0]
	}
	if providerRef == "" || strings.HasPrefix(providerRef, "-") || len(command.Args) > 1 {
		fmt.Fprintln(stderr, FormatCLIError(fmt.Sprintf("Usage: %s auth logout <provider>", appName)))
		return 1
	}

	refreshOnCreate := false
	runtime, err := CreateModelRuntime(CreateModelRuntimeOptions{
		AuthPath:          filepath.Join(GetAgentDir(), "auth.json"),
		AllowModelNetwork: false,
		RefreshOnCreate:   &refreshOnCreate,
	})
	if err != nil {
		fmt.Fprintln(stderr, FormatCLIError("Could not read stored credentials: "+err.Error()))
		return 1
	}
	ctx := context.Background()
	stored, err := runtime.ListCredentials(ctx)
	if err != nil {
		fmt.Fprintln(stderr, FormatCLIError("Could not read stored credentials: "+err.Error()))
		return 1
	}

	var provider *ai.Provider
	if matches := FindMatchingProviders(runtime.GetProviders(), providerRef); len(matches) > 0 {
		provider = matches[0]
	}
	// Providers registered by extensions are not loaded here, so their stored
	// credentials match by id alone.
	credentialID := providerRef
	if provider != nil {
		credentialID = provider.ID
	}
	var credential *ai.CredentialInfo
	for index := range stored {
		if stored[index].ProviderID == credentialID {
			credential = &stored[index]
			break
		}
	}
	if provider == nil && credential == nil {
		fmt.Fprintln(stderr, FormatCLIError(fmt.Sprintf("Unknown provider %q.", tui.SanitizeUntrustedText(providerRef))))
		ids := make([]string, 0, len(stored))
		for _, info := range stored {
			// A provider id comes from provider configuration, so it is data (D461).
			ids = append(ids, tui.SanitizeUntrustedText(info.ProviderID))
		}
		sort.Strings(ids)
		listed := "none"
		if len(ids) > 0 {
			listed = strings.Join(ids, ", ")
		}
		fmt.Fprintln(stderr, FormatCLIError("Stored credentials: "+listed))
		return 1
	}

	providerName := providerRef
	if provider != nil && provider.Name != "" {
		providerName = provider.Name
	}
	if credential == nil {
		fmt.Fprintln(stdout, fmt.Sprintf("No stored credentials for %s. Environment variables and models.json config are unchanged.", providerName))
		return 0
	}

	if err := runtime.Logout(credential.ProviderID, ctx); err != nil {
		var syncErr *CredentialSynchronizationError
		if errors.As(err, &syncErr) {
			fmt.Fprintln(stderr, FormatCLIError(fmt.Sprintf("Credentials removed for %s, but local model state could not be synchronized: %s", providerName, err.Error())))
		} else {
			fmt.Fprintln(stderr, FormatCLIError("Logout failed: "+err.Error()))
		}
		return 1
	}
	if credential.Type == ai.CredentialOAuth {
		fmt.Fprintln(stdout, fmt.Sprintf("Logged out of %s.", providerName))
	} else {
		fmt.Fprintln(stdout, fmt.Sprintf("Removed stored API key for %s. Environment variables and models.json config are unchanged.", providerName))
	}
	return 0
}
