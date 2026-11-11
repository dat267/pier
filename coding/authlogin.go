package coding

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/dat267/pier/ai"
)

// Port of pi v1.5.2 (b4afdddf) cli/auth-login.ts: `pier auth login <provider>`
// signs in with an account in the browser, or saves an API key, then exits.

// AuthLoginUsage is the usage line of `pier auth login`.
func AuthLoginUsage(appName string) string {
	return appName + " auth login <provider> [--api-key] [--no-browser]"
}

type authLoginRequest struct {
	providerRef string
	apiKey      bool
	browser     bool
}

// parseAuthLoginArgs mirrors the reference's parseLoginRequest.
func parseAuthLoginArgs(appName string, args []string) (authLoginRequest, string) {
	request := authLoginRequest{browser: true}
	usage := "Usage: " + AuthLoginUsage(appName)
	for _, arg := range args {
		switch {
		case arg == "--api-key":
			request.apiKey = true
		case arg == "--no-browser":
			request.browser = false
		case strings.HasPrefix(arg, "-") || request.providerRef != "":
			return request, usage
		default:
			request.providerRef = arg
		}
	}
	if request.providerRef == "" {
		return request, usage
	}
	if request.apiKey && !request.browser {
		return request, "--no-browser only applies to account sign-in; omit it with --api-key."
	}
	return request, ""
}

// RunAuthLogin implements `pier auth login`. The terminal is injected so tests
// can script the prompts.
func RunAuthLogin(appName string, args []string, terminal LoginTerminal, agentDir, cwd string, getDeviceID func() string) int {
	request, usageError := parseAuthLoginArgs(appName, args)
	if usageError != "" {
		terminal.Error(FormatCLIError(usageError))
		return 1
	}
	refreshOnCreate := false
	runtime, err := CreateModelRuntime(CreateModelRuntimeOptions{
		AuthPath:          filepath.Join(agentDir, "auth.json"),
		AllowModelNetwork: false,
		RefreshOnCreate:   &refreshOnCreate,
	})
	if err != nil {
		terminal.Error(FormatCLIError("Failed to prepare the model runtime: " + err.Error()))
		return 1
	}

	var provider *ai.Provider
	if matches := FindMatchingProviders(runtime.GetProviders(), request.providerRef); len(matches) > 0 {
		provider = matches[0]
	}
	if provider == nil {
		loginProviders := make([]string, 0, len(runtime.GetProviders()))
		for _, candidate := range runtime.GetProviders() {
			if candidate.Auth.OAuth != nil || (candidate.Auth.APIKey != nil && candidate.Auth.APIKey.Login != nil) {
				loginProviders = append(loginProviders, candidate.ID)
			}
		}
		sort.Strings(loginProviders)
		terminal.Error(FormatCLIError(fmt.Sprintf("Unknown provider %q.", ReplaceControlCharacterRuns(request.providerRef))))
		terminal.Error(FormatCLIError("Providers: " + strings.Join(loginProviders, ", ")))
		return 1
	}
	if !terminal.Interactive() {
		terminal.Error(FormatCLIError(fmt.Sprintf("%q requires an interactive terminal", AuthLoginUsage(appName))))
		return 1
	}

	authType, unavailable := selectLoginAuthType(provider, request.apiKey)
	if unavailable != "" {
		terminal.Error(FormatCLIError(unavailable))
		return 1
	}

	ctx := context.Background()
	handler := newLoginPromptHandler(appName, request, authType, provider.ID, terminal)
	interaction := &ai.AuthInteraction{
		Ctx:         ctx,
		Prompt:      handler.prompt,
		Notify:      handler.notify,
		GetDeviceID: getDeviceID,
	}
	if _, err := runtime.Login(provider.ID, authType, interaction); err != nil {
		var syncErr *CredentialSynchronizationError
		switch {
		case errors.As(err, &syncErr):
			terminal.Error(FormatCLIError(fmt.Sprintf("Logged in to %s, but local model state could not be synchronized: %s", provider.Name, err.Error())))
		case errors.Is(err, context.Canceled) || err.Error() == AuthLoginCancelledMessage:
			terminal.Error(FormatCLIError("Login cancelled."))
		default:
			terminal.Error(FormatCLIError(fmt.Sprintf("Failed to log in to %s: %s", provider.Name, err.Error())))
		}
		return 1
	}

	if authType == ai.CredentialOAuth {
		terminal.Write(fmt.Sprintf("Logged in to %s. Credentials saved to %s", provider.Name, filepath.Join(agentDir, "auth.json")))
	} else {
		terminal.Write(fmt.Sprintf("Saved API key for %s. Credentials saved to %s", provider.Name, filepath.Join(agentDir, "auth.json")))
	}
	refreshProviderCatalogAfterLogin(ctx, provider, runtime, terminal)
	if provider.ID == "radius" {
		configureRadiusMcpServerAfterLogin(provider, terminal, agentDir)
	}
	return 0
}

// selectLoginAuthType is the reference's selectAuthType: an account login unless
// apiKey asks for an API key, and providers without account login use their API
// key login.
func selectLoginAuthType(provider *ai.Provider, apiKey bool) (ai.AuthType, string) {
	hasAPIKeyLogin := provider.Auth.APIKey != nil && provider.Auth.APIKey.Login != nil
	if !apiKey && provider.Auth.OAuth != nil {
		return ai.CredentialOAuth, ""
	}
	if hasAPIKeyLogin {
		return ai.CredentialAPIKey, ""
	}
	if apiKey && provider.Auth.OAuth != nil {
		return "", fmt.Sprintf("%s has no API key login. Omit --api-key to sign in.", provider.Name)
	}
	method := "ambient credentials"
	if provider.Auth.APIKey != nil && provider.Auth.APIKey.Name != "" {
		method = provider.Auth.APIKey.Name
	}
	return "", fmt.Sprintf("%s is configured outside pier (%s).", provider.Name, method)
}

// loginPromptHandler adapts the provider's prompts and events to the command's
// selected sign-in method (the reference's createLoginInteraction).
type loginPromptHandler struct {
	appName  string
	request  authLoginRequest
	authType ai.AuthType
	provider string
	terminal LoginTerminal
	// methodChoice is "ask", "browser" or "other".
	methodChoice   string
	skippedMethods []string
}

func newLoginPromptHandler(appName string, request authLoginRequest, authType ai.AuthType, providerID string, terminal LoginTerminal) *loginPromptHandler {
	handler := &loginPromptHandler{appName: appName, request: request, authType: authType, provider: providerID, terminal: terminal}
	switch {
	case authType == ai.CredentialAPIKey:
		handler.methodChoice = "ask"
	case request.browser:
		handler.methodChoice = "browser"
	default:
		handler.methodChoice = "other"
	}
	return handler
}

func promptMessage(prompt ai.AuthPrompt) string {
	if prompt.Placeholder != "" {
		return prompt.Message + " (" + prompt.Placeholder + "): "
	}
	return prompt.Message + ": "
}

func (h *loginPromptHandler) prompt(prompt ai.AuthPrompt) (string, error) {
	ctx := context.Background()
	if prompt.Ctx != nil {
		ctx = prompt.Ctx
	}
	if prompt.Type == ai.AuthPromptSelect {
		return h.selectMethod(ctx, prompt)
	}
	if prompt.Type == ai.AuthPromptSecret {
		return h.terminal.Secret(ctx, promptMessage(prompt))
	}
	return h.terminal.Question(ctx, promptMessage(prompt))
}

// selectMethod picks a browser or terminal sign-in method: the browser one when
// --no-browser was not given, the only other one when the provider offers a
// single alternative, and a numbered choice otherwise.
func (h *loginPromptHandler) selectMethod(ctx context.Context, prompt ai.AuthPrompt) (string, error) {
	first := ""
	var others []ai.AuthSelectOption
	if len(prompt.SelectOptions) > 0 {
		first = prompt.SelectOptions[0].ID
		others = prompt.SelectOptions[1:]
	}
	if h.methodChoice == "ask" || first == "" {
		return selectInTerminal(ctx, prompt.Message, prompt.SelectOptions, h.terminal)
	}
	if h.methodChoice == "browser" {
		for _, option := range others {
			h.skippedMethods = append(h.skippedMethods, option.Label)
		}
		return first, nil
	}
	if len(others) == 0 {
		// A provider whose only method is the browser sign-in still gets it.
		return first, nil
	}
	if len(others) > 1 {
		return selectInTerminal(ctx, prompt.Message, others, h.terminal)
	}
	h.terminal.Write(prompt.Message + " " + others[0].Label)
	return others[0].ID, nil
}

// selectInTerminal prints the numbered options and reads a number.
func selectInTerminal(ctx context.Context, message string, options []ai.AuthSelectOption, terminal LoginTerminal) (string, error) {
	terminal.Write(message)
	for index, option := range options {
		line := fmt.Sprintf("  %d. %s", index+1, option.Label)
		if option.Description != "" {
			line += " - " + option.Description
		}
		terminal.Write(line)
	}
	for {
		answer, err := terminal.Question(ctx, fmt.Sprintf("Select 1-%d: ", len(options)))
		if err != nil {
			return "", err
		}
		answer = strings.TrimSpace(answer)
		index := 0
		if _, scanErr := fmt.Sscanf(answer, "%d", &index); scanErr == nil && index >= 1 && index <= len(options) {
			return options[index-1].ID, nil
		}
		terminal.Write(fmt.Sprintf("Enter a number from 1 to %d.", len(options)))
	}
}

func (h *loginPromptHandler) notify(event ai.AuthEvent) {
	switch event.Type {
	case ai.AuthEventAuthURL:
		h.terminal.Write("Open this URL to sign in:\n  " + DisplayURL(event.URL))
		if event.Instructions != "" {
			h.terminal.Write(event.Instructions)
		}
		if target, ok := WebURL(event.URL); ok && h.request.browser {
			h.terminal.OpenURL(target)
		}
		rerun := fmt.Sprintf("\"%s auth login %s --no-browser\"", h.appName, h.provider)
		switch len(h.skippedMethods) {
		case 0:
		case 1:
			h.terminal.Write(fmt.Sprintf("To use %q instead, run %s", h.skippedMethods[0], rerun))
		default:
			labels := make([]string, 0, len(h.skippedMethods))
			for _, label := range h.skippedMethods {
				labels = append(labels, fmt.Sprintf("%q", label))
			}
			h.terminal.Write(fmt.Sprintf("Other sign-in methods: %s. Run %s to choose one.", strings.Join(labels, ", "), rerun))
		}
	case ai.AuthEventDeviceCode:
		h.terminal.Write("Open " + DisplayURL(event.VerificationURI) + " and enter code: " + event.UserCode)
		if target, ok := WebURL(event.VerificationURI); ok && h.request.browser {
			h.terminal.OpenURL(target)
		}
		h.terminal.Write("Waiting for authentication...")
	case ai.AuthEventInfo:
		h.terminal.Write(event.Message)
		for _, link := range event.Links {
			if link.Label != "" {
				h.terminal.Write("  " + link.Label + ": " + DisplayURL(link.URL))
			} else {
				h.terminal.Write("  " + DisplayURL(link.URL))
			}
		}
	default:
		h.terminal.Write(event.Message)
	}
}

// refreshProviderCatalogAfterLogin fetches the provider's catalog now, because a
// dynamic catalog stays empty until its first authenticated refresh.
func refreshProviderCatalogAfterLogin(ctx context.Context, provider *ai.Provider, runtime *ModelRuntime, terminal LoginTerminal) {
	refreshCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	result, err := runtime.Refresh(refreshCtx, &ModelsRefreshCallOptions{Providers: []string{provider.ID}})
	if err != nil {
		terminal.Write("The " + provider.Name + " model catalog could not be refreshed: " + err.Error())
		return
	}
	if result.Aborted {
		terminal.Write("The " + provider.Name + " model catalog refresh timed out; " + AppName + " refreshes it on next start.")
		return
	}
	if len(result.Errors) > 0 {
		terminal.Write("The " + provider.Name + " model catalog could not be refreshed; " + AppName + " retries on next start.")
	}
}

// configureRadiusMcpServerAfterLogin points the global Radius MCP server at the
// Radius login without asking, since the login is what the server needs
// (pi v1.5.2 configureRadiusMcpServer).
func configureRadiusMcpServerAfterLogin(provider *ai.Provider, terminal LoginTerminal, agentDir string) {
	name, config := PrepareRadiusMcpServerConfig(agentDir)
	if name == "" {
		return
	}
	path := filepath.Join(agentDir, "mcp.json")
	replaced, err := AddMcpServerConfig(path, name, config)
	if err != nil {
		terminal.Error("Could not update " + path + ": " + err.Error())
		return
	}
	verb := "Added"
	if replaced {
		verb = "Updated"
	}
	terminal.Write(fmt.Sprintf("%s MCP server %q in %s to use the %s login.", verb, name, path, provider.Name))
}
