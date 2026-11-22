package ai

import (
	"bytes"
	ctxpkg "context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// Port of api/azure-openai-responses.ts.

// DefaultAzureAPIVersion is the Azure OpenAI API version the adapter uses.
const DefaultAzureAPIVersion = "v1"

// azureToolCallProviders accepts the `call_id|item_id` tool-call id form.
var azureToolCallProviders = map[string]bool{
	"openai": true, "openai-codex": true, "opencode": true, "azure": true,
}

// AzureOpenAIResponsesOptions are the Azure Responses stream options.
type AzureOpenAIResponsesOptions struct {
	OpenAIResponsesOptions
	AzureAPIVersion     string
	AzureResourceName   string
	AzureBaseURL        string
	AzureDeploymentName string
}

// ParseDeploymentNameMap parses AZURE_OPENAI_DEPLOYMENT_NAME_MAP
// ("model=deployment,...").
func ParseDeploymentNameMap(value string) map[string]string {
	result := map[string]string{}
	for entry := range strings.SplitSeq(value, ",") {
		trimmed := JSTrim(entry)
		if trimmed == "" {
			continue
		}
		parts := strings.SplitN(trimmed, "=", 2)
		if len(parts) != 2 || JSTrim(parts[0]) == "" || JSTrim(parts[1]) == "" {
			continue
		}
		result[JSTrim(parts[0])] = JSTrim(parts[1])
	}
	return result
}

// ResolveAzureDeploymentName resolves the deployment name for a model.
func ResolveAzureDeploymentName(model *Model, options *AzureOpenAIResponsesOptions) string {
	if options != nil && options.AzureDeploymentName != "" {
		return options.AzureDeploymentName
	}
	env := ProviderEnv(nil)
	if options != nil {
		env = options.Env
	}
	mapped := ParseDeploymentNameMap(GetProviderEnvValueOr("AZURE_OPENAI_DEPLOYMENT_NAME_MAP", env))
	if deployment, ok := mapped[model.ID]; ok {
		return deployment
	}
	return model.ID
}

// NormalizeAzureBaseURL normalizes an Azure endpoint so the v1 surface can
// append `/responses` (upstream normalizeAzureBaseUrl).
func NormalizeAzureBaseURL(baseURL string) (string, error) {
	trimmed := strings.TrimRight(JSTrim(baseURL), "/")
	parsed, err := url.Parse(trimmed)
	// Go's url.Parse accepts relative input, so require an absolute http(s)
	// URL the way the upstream URL constructor does.
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return "", fmt.Errorf("Invalid Azure OpenAI base URL: %s", baseURL)
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	hostname := strings.ToLower(parsed.Hostname())
	isAzureHost := strings.HasSuffix(hostname, ".openai.azure.com") ||
		strings.HasSuffix(hostname, ".cognitiveservices.azure.com") ||
		strings.HasSuffix(hostname, ".ai.azure.com")
	host := hostname
	port := parsed.Port()
	// The WHATWG URL parser reads a port as a number, so leading zeros are not
	// part of it: :0443 on https is the default 443 and is dropped, while :0080
	// on https is port 80 and survives as ":80". Compare and rewrite the
	// numeric value instead of the literal text.
	if port != "" {
		numeric, err := strconv.Atoi(port)
		// The WHATWG URL parser rejects a port above 65535; url.Parse keeps the
		// text, so check it here rather than pass an unusable port through.
		if err != nil || numeric > 65535 {
			return "", fmt.Errorf("Invalid Azure OpenAI base URL: %s", baseURL)
		}
		if (parsed.Scheme == "http" && numeric == 80) || (parsed.Scheme == "https" && numeric == 443) {
			port = ""
		} else {
			port = strconv.Itoa(numeric)
		}
	}
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	if port != "" {
		host += ":" + port
	}
	parsed.Host = host
	// A WHATWG URL always has a path: with no path but a query or fragment the
	// serialization is `host/?q`, so record `/` rather than leave Path empty and
	// emit `host?q`.
	if parsed.Path == "" && parsed.RawPath == "" {
		parsed.Path = "/"
	}
	// The WHATWG parser resolves "." and ".." path segments while parsing, so
	// `/a/../v1` is `/v1` and a trailing `/v1/..` ends at the root. Go keeps the
	// segments as written, which would send the request to a different path.
	if resolved := resolveDotSegments(parsed.EscapedPath()); resolved != parsed.EscapedPath() {
		if unescaped, err := url.PathUnescape(resolved); err == nil {
			parsed.Path = unescaped
			parsed.RawPath = ""
		}
	}
	normalizedPath := strings.TrimRight(parsed.EscapedPath(), "/")

	// Azure hosts get /openai/v1 as the base path so the SDK-style request
	// layout (`{base}/responses?api-version=v1`) works.
	if isAzureHost && (normalizedPath == "" || normalizedPath == "/" ||
		normalizedPath == "/openai" || normalizedPath == "/openai/v1/responses") {
		parsed.Path = "/openai/v1"
		parsed.RawPath = ""
		parsed.RawQuery = ""
	}
	return strings.TrimRight(parsed.String(), "/"), nil
}

// resolveDotSegments applies the WHATWG path-segment rules: a "." segment is
// dropped and a ".." segment removes the segment before it. A trailing "." or
// ".." leaves a trailing slash, and ".." at the root stays at the root.
func resolveDotSegments(p string) string {
	if p == "" || (!strings.Contains(p, "/.") && !strings.HasSuffix(p, ".")) {
		return p
	}
	leadingSlash := strings.HasPrefix(p, "/")
	trailingSlash := strings.HasSuffix(p, "/") || strings.HasSuffix(p, "/.") || strings.HasSuffix(p, "/..")
	segments := strings.Split(p, "/")
	stack := make([]string, 0, len(segments))
	for _, segment := range segments {
		switch segment {
		case "", ".":
			continue
		case "..":
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		default:
			stack = append(stack, segment)
		}
	}
	result := strings.Join(stack, "/")
	if leadingSlash {
		result = "/" + result
	}
	if trailingSlash && !strings.HasSuffix(result, "/") {
		result += "/"
	}
	return result
}

// BuildDefaultAzureBaseURL builds the resource-name base URL.
func BuildDefaultAzureBaseURL(resourceName string) string {
	return fmt.Sprintf("https://%s.openai.azure.com/openai/v1", resourceName)
}

// ResolveAzureConfig resolves the Azure base URL and API version
// (upstream resolveAzureConfig).
func ResolveAzureConfig(model *Model, options *AzureOpenAIResponsesOptions) (string, string, error) {
	env := ProviderEnv(nil)
	if options != nil {
		env = options.Env
	}
	apiVersion := DefaultAzureAPIVersion
	if options != nil && options.AzureAPIVersion != "" {
		apiVersion = options.AzureAPIVersion
	} else if value := GetProviderEnvValueOr("AZURE_OPENAI_API_VERSION", env); value != "" {
		apiVersion = value
	}

	baseURL := ""
	if options != nil {
		baseURL = JSTrim(options.AzureBaseURL)
	}
	if baseURL == "" {
		baseURL = JSTrim(GetProviderEnvValueOr("AZURE_OPENAI_BASE_URL", env))
	}
	resourceName := ""
	if options != nil {
		resourceName = options.AzureResourceName
	}
	if resourceName == "" {
		resourceName = GetProviderEnvValueOr("AZURE_OPENAI_RESOURCE_NAME", env)
	}

	resolved := baseURL
	if resolved == "" && resourceName != "" {
		resolved = BuildDefaultAzureBaseURL(resourceName)
	}
	if resolved == "" {
		resolved = model.BaseURL
	}
	if resolved == "" {
		return "", "", fmt.Errorf("Azure OpenAI base URL is required. Set AZURE_OPENAI_BASE_URL or " +
			"AZURE_OPENAI_RESOURCE_NAME, or pass azureBaseUrl, azureResourceName, or model.baseUrl.")
	}
	normalized, err := NormalizeAzureBaseURL(resolved)
	if err != nil {
		return "", "", err
	}
	return normalized, apiVersion, nil
}

// StreamAzureOpenAIResponses streams an Azure OpenAI Responses call
// (upstream stream).
func StreamAzureOpenAIResponses(model *Model, context TranscriptContext, options *AzureOpenAIResponsesOptions) *AssistantMessageEventStream {
	responsesOptions := &OpenAIResponsesOptions{}
	if options != nil {
		responsesOptions = &options.OpenAIResponsesOptions
	}
	config := &openAIResponsesStreamConfig{
		errorPrefix:         "Azure OpenAI API error",
		noStopReasonMessage: "Azure OpenAI Responses stream ended without a stop reason",
		toolCallProviders:   azureToolCallProviders,
		modelName: func(model *Model, _ *OpenAIResponsesOptions) string {
			return ResolveAzureDeploymentName(model, options)
		},
		buildRequest: func(
			ctx ctxpkg.Context, model *Model, body []byte, apiKey string,
			requestOptions *OpenAIResponsesOptions, compat ResolvedOpenAIResponsesCompat,
			cacheSessionID string, messages []Message,
		) (*http.Request, error) {
			azureOptions := &AzureOpenAIResponsesOptions{}
			if options != nil {
				*azureOptions = *options
			}
			if requestOptions != nil {
				azureOptions.OpenAIResponsesOptions = *requestOptions
			}
			baseURL, apiVersion, err := ResolveAzureConfig(model, azureOptions)
			if err != nil {
				return nil, err
			}
			requestURL := baseURL + "/responses?api-version=" + url.QueryEscape(apiVersion)
			req, err := http.NewRequestWithContext(ctx, http.MethodPost, requestURL, bytes.NewReader(body))
			if err != nil {
				return nil, err
			}
			req.Header.Set("Content-Type", "application/json")
			ApplyIdentityHeaders(req.Header, model, requestOptions.Identity)
			// The Azure OpenAI SDK authenticates with the api-key header.
			if apiKey != "" {
				req.Header.Set("api-key", apiKey)
			}
			// No session-affinity headers: the Azure dialect has no such
			// concept, and upstream's client sets only identity, model and
			// caller headers (api/azure-openai-responses.ts createClient).
			for name, value := range model.Headers {
				req.Header.Set(name, value)
			}
			applyProviderHeaders(req.Header, requestOptions.Headers)
			return req, nil
		},
	}
	return streamOpenAIResponses(model, context, responsesOptions, config)
}

// StreamAzureOpenAIResponsesSimple maps reasoning levels onto the Responses
// reasoning effort (upstream streamSimple).
func StreamAzureOpenAIResponsesSimple(model *Model, context TranscriptContext, options *SimpleStreamOptions) *AssistantMessageEventStream {
	stream, options, ok := startSimpleStream(model, options)
	if !ok {
		return stream
	}
	go func() {
		responsesOptions := &OpenAIResponsesOptions{StreamOptions: withSimpleMaxTokens(model, context, options.StreamOptions)}
		if options.ToolChoice != nil {
			responsesOptions.ToolChoice = mustMarshalJSON(*options.ToolChoice)
		}
		if options.Reasoning != "" {
			clamped := ClampThinkingLevel(model, options.Reasoning)
			if clamped != ThinkOff {
				responsesOptions.ReasoningEffort = clamped
			}
		}
		azureOptions := &AzureOpenAIResponsesOptions{OpenAIResponsesOptions: *responsesOptions}
		forwardStream(stream, StreamAzureOpenAIResponses(model, context, azureOptions))
	}()
	return stream
}
