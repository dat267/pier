package ai

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// Port of api/openrouter-images.ts: OpenRouter's image generation through the
// chat completions endpoint (modalities: ["image"]).

// openRouterImagesEndpoint is overridable for tests.
var openRouterImagesEndpoint = "/chat/completions"

// OpenRouterImages implements ProviderImages.
type OpenRouterImages struct{}

// GenerateImages runs one image generation (upstream generateImages).
func (OpenRouterImages) GenerateImages(model *ImagesModel, context ImagesContext, options *ImagesOptions) *AssistantImages {
	output := &AssistantImages{
		API: model.API, Provider: model.Provider, Model: model.ID,
		Output: []ImagesOutputContent{}, StopReason: ImagesStopStop,
		Timestamp: time.Now().UnixMilli(),
	}
	if options == nil {
		options = &ImagesOptions{}
	}
	if options.APIKey == "" {
		message := fmt.Sprintf("No API key for provider: %s", model.Provider)
		output.StopReason = ImagesStopError
		output.ErrorMessage = &message
		return output
	}

	// Upstream passes the image model itself to both hooks.
	hookModel := &Model{
		ID: model.ID, Name: model.Name, API: model.API, Provider: model.Provider, BaseURL: model.BaseURL,
	}
	var params any = buildOpenRouterImagesParams(model, context)
	if options.OnPayload != nil {
		payload := mustMarshalJSON(params)
		if next := options.OnPayload(payload, hookModel); next != nil {
			// Upstream sends a replacement verbatim; a map round trip would
			// re-sort every member and drop unknown nested order.
			params = next
		}
	}
	body, err := MarshalJSON(params)
	if err != nil {
		message := err.Error()
		output.StopReason = ImagesStopError
		output.ErrorMessage = &message
		return output
	}

	ctx := bgCtx()
	if options.Ctx != nil {
		ctx = options.Ctx
	}
	requestURL := strings.TrimRight(model.BaseURL, "/") + openRouterImagesEndpoint
	response, err := RetryProviderRequest(ctx, func() (*http.Response, error) {
		request, nerr := http.NewRequestWithContext(ctx, http.MethodPost, requestURL, bytes.NewReader(body))
		if nerr != nil {
			return nil, nerr
		}
		request.Header.Set("Content-Type", "application/json")
		for name, value := range ImagesIdentityHeaders(model, options.Identity) {
			request.Header.Set(name, value)
		}
		request.Header.Set("Authorization", "Bearer "+options.APIKey)
		for name, value := range model.Headers {
			request.Header.Set(name, value)
		}
		applyProviderHeaders(request.Header, options.Headers)
		hresp, rerr := DoProviderRequestWithSDKTimeoutAndIdle(request, options.TimeoutMs, options.HTTPIdleTimeoutMs)
		if rerr != nil {
			return nil, rerr
		}
		if hresp.StatusCode >= 400 {
			raw, _ := io.ReadAll(io.LimitReader(hresp.Body, 1<<20))
			hresp.Body.Close()
			message, body := openAIAPIError(hresp.StatusCode, raw)
			return nil, &ProviderError{
				Status: hresp.StatusCode, Headers: hresp.Header,
				Message: message,
				Body:    body,
			}
		}
		return hresp, nil
	}, &ProviderRetryOptions{MaxRetries: options.MaxRetries, MaxRetryDelayMS: options.MaxRetryDelayMs})
	if err != nil {
		if options.Ctx != nil && options.Ctx.Err() != nil {
			output.StopReason = ImagesStopAborted
		} else {
			output.StopReason = ImagesStopError
		}
		message := FormatProviderError(NormalizeProviderError(err), "")
		output.ErrorMessage = &message
		return output
	}
	defer response.Body.Close()
	if options.OnResponse != nil {
		headers := map[string]string{}
		for name, values := range response.Header {
			headers[strings.ToLower(name)] = strings.Join(values, ", ")
		}
		options.OnResponse(ProviderResponse{Status: response.StatusCode, Headers: headers}, hookModel)
	}

	raw, _ := io.ReadAll(io.LimitReader(response.Body, 8<<20))
	var payload struct {
		ID      string `json:"id"`
		Choices []struct {
			Message struct {
				Content json.RawMessage `json:"content"`
				Images  []struct {
					ImageURL json.RawMessage `json:"image_url"`
				} `json:"images"`
			} `json:"message"`
		} `json:"choices"`
		Usage json.RawMessage `json:"usage"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		message := err.Error()
		output.StopReason = ImagesStopError
		output.ErrorMessage = &message
		return output
	}
	output.ResponseID = &payload.ID
	if len(payload.Usage) > 0 {
		var usage struct {
			PromptTokens        float64 `json:"prompt_tokens"`
			CompletionTokens    float64 `json:"completion_tokens"`
			PromptTokensDetails *struct {
				CachedTokens     *float64 `json:"cached_tokens"`
				CacheWriteTokens *float64 `json:"cache_write_tokens"`
			} `json:"prompt_tokens_details"`
		}
		if err := json.Unmarshal(payload.Usage, &usage); err == nil {
			computed := parseOpenRouterImagesUsage(usage, model)
			output.Usage = &computed
		}
	}
	if len(payload.Choices) > 0 {
		choice := payload.Choices[0]
		var text string
		if err := json.Unmarshal(choice.Message.Content, &text); err == nil && text != "" {
			output.Output = append(output.Output, TextContent{Text: text})
		}
		for _, image := range choice.Message.Images {
			imageURL := ""
			if err := json.Unmarshal(image.ImageURL, &imageURL); err != nil {
				var record struct {
					URL string `json:"url"`
				}
				if err := json.Unmarshal(image.ImageURL, &record); err == nil {
					imageURL = record.URL
				}
			}
			if !strings.HasPrefix(imageURL, "data:") {
				continue
			}
			matches := openRouterDataURLPattern.FindStringSubmatch(imageURL)
			if matches == nil {
				continue
			}
			output.Output = append(output.Output, ImageContent{MimeType: matches[1], Data: matches[2]})
		}
	}
	return output
}

var openRouterDataURLPattern = regexp.MustCompile(`^data:([^;]+);base64,(.+)$`)

// openRouterImagesParams mirrors the upstream object literal, whose member
// order is the request wire order.
type openRouterImagesParams struct {
	Model      string                    `json:"model"`
	Messages   []openRouterImagesMessage `json:"messages"`
	Stream     bool                      `json:"stream"`
	Modalities []string                  `json:"modalities"`
}

type openRouterImagesMessage struct {
	Role    string `json:"role"`
	Content []any  `json:"content"`
}

type openRouterImagesTextPart struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type openRouterImagesURL struct {
	URL string `json:"url"`
}

type openRouterImagesImagePart struct {
	Type     string              `json:"type"`
	ImageURL openRouterImagesURL `json:"image_url"`
}

// openAIAPIError reproduces the openai SDK's APIError message and the body
// normalizeProviderError reports: the parsed `error` object when the body is a
// JSON envelope, otherwise the raw text. The message is "<status> <text>",
// or "<status> status code (no body)" for an empty body.
func openAIAPIError(status int, raw []byte) (string, string) {
	text := JSTrim(string(raw))
	if text == "" {
		return fmt.Sprintf("%d status code (no body)", status), ""
	}
	// The SDK parses `errorResponse.error` and uses its string `message` (or the
	// stringified object) in the APIError message.
	var envelope struct {
		Error json.RawMessage `json:"error"`
	}
	if err := json.Unmarshal(raw, &envelope); err == nil && len(envelope.Error) > 0 {
		var inner map[string]json.RawMessage
		if err := json.Unmarshal(envelope.Error, &inner); err == nil && len(inner) > 0 {
			var compact bytes.Buffer
			if json.Compact(&compact, envelope.Error) == nil {
				descriptor := compact.String()
				if message, ok := inner["message"]; ok {
					var text string
					if err := json.Unmarshal(message, &text); err == nil {
						descriptor = text
					}
				}
				return fmt.Sprintf("%d %s", status, descriptor), compact.String()
			}
		}
	}
	return fmt.Sprintf("%d %s", status, text), text
}

// buildOpenRouterImagesParams renders the chat completions payload.
func buildOpenRouterImagesParams(model *ImagesModel, context ImagesContext) openRouterImagesParams {
	content := make([]any, 0, len(context.Input))
	for _, item := range context.Input {
		switch typed := item.(type) {
		case TextContent:
			content = append(content, openRouterImagesTextPart{Type: "text", Text: SanitizeSurrogates(typed.Text)})
		case ImageContent:
			content = append(content, openRouterImagesImagePart{
				Type:     "image_url",
				ImageURL: openRouterImagesURL{URL: "data:" + typed.MimeType + ";base64," + typed.Data},
			})
		}
	}
	modalities := []string{"image"}
	if containsString(model.Output, "text") {
		modalities = []string{"image", "text"}
	}
	return openRouterImagesParams{
		Model:      model.ID,
		Messages:   []openRouterImagesMessage{{Role: "user", Content: content}},
		Stream:     false,
		Modalities: modalities,
	}
}

// parseOpenRouterImagesUsage derives usage and cost (upstream parseUsage).
func parseOpenRouterImagesUsage(raw struct {
	PromptTokens        float64 `json:"prompt_tokens"`
	CompletionTokens    float64 `json:"completion_tokens"`
	PromptTokensDetails *struct {
		CachedTokens     *float64 `json:"cached_tokens"`
		CacheWriteTokens *float64 `json:"cache_write_tokens"`
	} `json:"prompt_tokens_details"`
}, model *ImagesModel) Usage {
	promptTokens := int64(raw.PromptTokens)
	reportedCached := int64(0)
	cacheWrite := int64(0)
	if raw.PromptTokensDetails != nil {
		if raw.PromptTokensDetails.CachedTokens != nil {
			reportedCached = int64(*raw.PromptTokensDetails.CachedTokens)
		}
		if raw.PromptTokensDetails.CacheWriteTokens != nil {
			cacheWrite = int64(*raw.PromptTokensDetails.CacheWriteTokens)
		}
	}
	cacheRead := reportedCached
	if cacheWrite > 0 {
		cacheRead = max(0, reportedCached-cacheWrite)
	}
	inputTokens := max(0, promptTokens-cacheRead-cacheWrite)
	outputTokens := int64(raw.CompletionTokens)
	usage := Usage{
		Input: inputTokens, Output: outputTokens,
		CacheRead: cacheRead, CacheWrite: cacheWrite,
		TotalTokens: inputTokens + outputTokens + cacheRead + cacheWrite,
		Cost: UsageCost{
			Input:      model.Cost.Input / 1000000 * float64(inputTokens),
			Output:     model.Cost.Output / 1000000 * float64(outputTokens),
			CacheRead:  model.Cost.CacheRead / 1000000 * float64(cacheRead),
			CacheWrite: model.Cost.CacheWrite / 1000000 * float64(cacheWrite),
		},
	}
	usage.Cost.Total = usage.Cost.Input + usage.Cost.Output + usage.Cost.CacheRead + usage.Cost.CacheWrite
	return usage
}

// OpenRouterImagesProvider builds the built-in OpenRouter image provider
// (upstream openrouterImagesProvider).
func OpenRouterImagesProvider() *ImagesProvider {
	return CreateImagesProvider(ImageProviderOptions{
		ID:   "openrouter",
		Name: "OpenRouter",
		Auth: ProviderAuth{
			APIKey: EnvApiKeyAuth("OpenRouter API key", []string{"OPENROUTER_API_KEY"}),
			OAuth:  OpenRouterOAuth(),
		},
		Models: GetBuiltinImageModels("openrouter"),
		API:    OpenRouterImages{},
	})
}

// BuiltinImagesProviders lists every built-in image provider (upstream
// builtinImagesProviders).
func BuiltinImagesProviders() []*ImagesProvider {
	return []*ImagesProvider{OpenRouterImagesProvider()}
}

// BuiltinImagesModels registers every built-in image provider (upstream
// builtinImagesModels).
func BuiltinImagesModels(models *ImagesModels) *ImagesModels {
	for _, provider := range BuiltinImagesProviders() {
		models.SetProvider(provider)
	}
	return models
}
