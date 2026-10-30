package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strings"
)

const systemOneLabel = "System One API"

// TypeSafeSystemOne implements TypeSafe's classifier protocol, also served by
// OpenRouter, OpenCode and Vercel AI Gateway (pi v1.4.0 system-one-shared.ts).
type TypeSafeSystemOne struct{}

func (TypeSafeSystemOne) Classify(model *Model, classifierContext ClassifierContext, options *ClassifierOptions) (result *ClassifierResult) {
	if model.API != APITypeSafeSystemOne {
		return classifierErrorResult(model, fmt.Errorf("Unsupported classifier API: %s", model.API), options)
	}
	if options == nil {
		options = &ClassifierOptions{}
	}
	result = &ClassifierResult{
		API: model.API, Provider: model.Provider, Model: model.ID,
		Answers: map[string]ClassifierAnswer{}, StopReason: ClassifierStopStop,
		Timestamp: nowUnixMilli(),
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			result = classifierFail(result, fmt.Errorf("%v", recovered), options)
		}
	}()
	if len(classifierContext.Images) > 0 && !containsString(model.Input, "image") {
		return classifierFail(result, fmt.Errorf("%s does not support image input for %s", systemOneLabel, model.ID), options)
	}
	body, err := buildSystemOneRequest(model, classifierContext)
	if err != nil {
		return classifierFail(result, err, options)
	}
	if options.APIKey == "" {
		return classifierFail(result, fmt.Errorf("No API key for provider: %s", model.Provider), options)
	}
	payload, err := body.marshal()
	if err != nil {
		return classifierFail(result, err, options)
	}
	if options.OnPayload != nil {
		if transformed := options.OnPayload(payload, model); transformed != nil {
			payload = transformed
		}
	}
	ctx := options.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	endpoint, err := systemOneURL(model.BaseURL)
	if err != nil {
		return classifierFail(result, err, options)
	}
	maxRetries := 2
	if options.MaxRetries != nil {
		maxRetries = *options.MaxRetries
	}
	response, err := RetryProviderRequest(ctx, func() (*http.Response, error) {
		request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(string(payload)))
		if err != nil {
			return nil, err
		}
		request.Header.Set("Content-Type", "application/json")
		ApplyIdentityHeaders(request.Header, model, options.Identity)
		request.Header.Set("Authorization", "Bearer "+options.APIKey)
		for name, value := range model.Headers {
			request.Header.Set(name, value)
		}
		applyProviderHeaders(request.Header, systemOneRequestHeaders(model, classifierContext.Images, options))
		response, err := DoProviderRequestWithAbortSignalTimeoutAndIdle(request, options.TimeoutMs, options.HTTPIdleTimeoutMs)
		if err != nil {
			return nil, err
		}
		if response.StatusCode >= 200 && response.StatusCode < 300 {
			return response, nil
		}
		responseBody, readErr := io.ReadAll(io.LimitReader(response.Body, 1<<20))
		_ = response.Body.Close()
		if readErr != nil {
			return nil, readErr
		}
		return nil, &ProviderError{
			Status: response.StatusCode, Headers: response.Header.Clone(),
			Message: fmt.Sprintf("%s returned %d", systemOneLabel, response.StatusCode), Body: string(responseBody),
		}
	}, &ProviderRetryOptions{MaxRetries: &maxRetries, MaxRetryDelayMS: options.MaxRetryDelayMs})
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return classifierFail(result, err, options)
		}
		if errors.Is(err, context.DeadlineExceeded) && ctx.Err() == nil && options.TimeoutMs != nil {
			return classifierFail(result, fmt.Errorf("Request timed out after %dms", *options.TimeoutMs), options)
		}
		return classifierFail(result, fmt.Errorf("%s", FormatProviderError(NormalizeProviderError(err), systemOneLabel+" error")), options)
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(response.Body)
	if err != nil {
		return classifierFail(result, err, options)
	}
	responseFields, err := decodeOrderedJSONFields(responseBody)
	if err != nil {
		return classifierFail(result, fmt.Errorf("%s returned an unexpected response", systemOneLabel), options)
	}
	answersRaw, _ := responseFields.raw("answers")
	usageRaw, _ := responseFields.raw("usage")
	result.Usage = parseSystemOneUsage(usageRaw, model)
	if options.OnResponse != nil {
		options.OnResponse(ProviderResponse{Status: response.StatusCode, Headers: responseHeaders(response.Header)}, model)
	}
	answers, order, err := parseSystemOneAnswers(answersRaw, classifierContext.Questions)
	if err != nil {
		return classifierFail(result, err, options)
	}
	result.Answers, result.AnswerOrder = answers, order
	return result
}

func systemOneURL(base string) (string, error) {
	parsed, err := url.Parse(strings.TrimRight(base, "/") + "/")
	if err != nil {
		return "", err
	}
	return parsed.ResolveReference(&url.URL{Path: "systemone"}).String(), nil
}

func systemOneRequestHeaders(model *Model, images []ImageContent, options *ClassifierOptions) ProviderHeaders {
	sendOptIn := model.Provider != "openrouter" && !strings.Contains(model.BaseURL, "openrouter.ai")
	if model.Compat != nil && model.Compat.SystemOne != nil && model.Compat.SystemOne.SendImageOptInHeader != nil {
		sendOptIn = *model.Compat.SystemOne.SendImageOptInHeader
	}
	if len(images) == 0 || !sendOptIn {
		return options.Headers
	}
	optIn := "true"
	headers := ProviderHeaders{"TypeSafe-Danger-Zone-Images": &optIn}
	for name, value := range options.Headers {
		headers[name] = value
	}
	return headers
}

func buildSystemOneRequest(model *Model, classifierContext ClassifierContext) (*orderedJSONFields, error) {
	state := classifierContext.State
	if len(state) == 0 {
		state = json.RawMessage("{}")
	}
	if !json.Valid(state) {
		return nil, fmt.Errorf("%s state must be valid JSON", systemOneLabel)
	}
	questions := &orderedJSONFields{values: map[string]json.RawMessage{}}
	for _, question := range classifierContext.Questions {
		wireType := question.Type
		var criteria any
		switch question.Type {
		case "choice":
			choices := &orderedJSONFields{values: map[string]json.RawMessage{}}
			for _, choice := range question.Choices {
				if err := choices.setAny(choice.Value, choice.Description); err != nil {
					return nil, err
				}
			}
			criteria = choices
		case "score":
			criteria = question.Levels
		case "bool":
			wireType = "noul"
			boolCriteria := &orderedJSONFields{values: map[string]json.RawMessage{}}
			if err := boolCriteria.setAny("true", question.BoolCriteria.True); err != nil {
				return nil, err
			}
			if err := boolCriteria.setAny("false", question.BoolCriteria.False); err != nil {
				return nil, err
			}
			criteria = boolCriteria
		default:
			return nil, fmt.Errorf("%s received invalid question type %q", systemOneLabel, question.Type)
		}
		questionFields := &orderedJSONFields{values: map[string]json.RawMessage{}}
		if err := questionFields.setAny("type", wireType); err != nil {
			return nil, err
		}
		if err := questionFields.setAny("instructions", question.Instructions); err != nil {
			return nil, err
		}
		if criteriaObject, ok := criteria.(*orderedJSONFields); ok {
			encoded, err := criteriaObject.marshal()
			if err != nil {
				return nil, err
			}
			questionFields.set("criteria", encoded)
		} else if err := questionFields.setAny("criteria", criteria); err != nil {
			return nil, err
		}
		encoded, err := questionFields.marshal()
		if err != nil {
			return nil, err
		}
		questions.set(question.Name, encoded)
	}
	request := &orderedJSONFields{values: map[string]json.RawMessage{}}
	if err := request.setAny("model", model.ID); err != nil {
		return nil, err
	}
	request.set("state", state)
	questionJSON, err := questions.marshal()
	if err != nil {
		return nil, err
	}
	request.set("questions", questionJSON)
	if len(classifierContext.Images) > 0 {
		compat := systemOneImageMode(model)
		switch compat {
		case "images":
			if len(classifierContext.Images) > 4 {
				return nil, fmt.Errorf("%s accepts at most 4 images for %s, got %d", systemOneLabel, model.ID, len(classifierContext.Images))
			}
			dataURLs := make([]string, 0, len(classifierContext.Images))
			for _, image := range classifierContext.Images {
				if image.MimeType != "image/png" && image.MimeType != "image/jpeg" && image.MimeType != "image/webp" {
					return nil, fmt.Errorf("%s accepts PNG, JPEG, and WebP images for %s, got %s", systemOneLabel, model.ID, image.MimeType)
				}
				dataURLs = append(dataURLs, "data:"+image.MimeType+";base64,"+image.Data)
			}
			if err := request.setAny("images", dataURLs); err != nil {
				return nil, err
			}
		case "state-image-urls":
			parts := make([]json.RawMessage, 0, len(classifierContext.Images)+1)
			for _, image := range classifierContext.Images {
				imageURL := &orderedJSONFields{values: map[string]json.RawMessage{}}
				if err := imageURL.setAny("url", "data:"+image.MimeType+";base64,"+image.Data); err != nil {
					return nil, err
				}
				imageURLJSON, err := imageURL.marshal()
				if err != nil {
					return nil, err
				}
				part := &orderedJSONFields{values: map[string]json.RawMessage{}}
				if err := part.setAny("type", "image_url"); err != nil {
					return nil, err
				}
				part.set("image_url", imageURLJSON)
				partJSON, err := part.marshal()
				if err != nil {
					return nil, err
				}
				parts = append(parts, partJSON)
			}
			parts = append(parts, json.RawMessage(state))
			stateWithImages, err := MarshalJSON(parts)
			if err != nil {
				return nil, err
			}
			request.set("state", stateWithImages)
		default:
			assets := &orderedJSONFields{values: map[string]json.RawMessage{}}
			keys := make([]string, 0, len(classifierContext.Images))
			for index, image := range classifierContext.Images {
				format := ""
				switch image.MimeType {
				case "image/png":
					format = "png"
				case "image/jpeg":
					format = "jpeg"
				default:
					return nil, fmt.Errorf("%s accepts PNG and JPEG images for %s, got %s", systemOneLabel, model.ID, image.MimeType)
				}
				name := fmt.Sprintf("<image-%d>", index+1)
				keys = append(keys, name)
				asset := &orderedJSONFields{values: map[string]json.RawMessage{}}
				if err := asset.setAny("type", "image"); err != nil {
					return nil, err
				}
				if err := asset.setAny("format", format); err != nil {
					return nil, err
				}
				if err := asset.setAny("data_base64", image.Data); err != nil {
					return nil, err
				}
				encoded, err := asset.marshal()
				if err != nil {
					return nil, err
				}
				assets.set(name, encoded)
			}
			assetJSON, err := assets.marshal()
			if err != nil {
				return nil, err
			}
			request.set("assets", assetJSON)
			stateWithImages, err := MarshalJSON(appendImageNames(keys, state))
			if err != nil {
				return nil, err
			}
			request.set("state", stateWithImages)
		}
	}
	return request, nil
}

func systemOneImageMode(model *Model) string {
	if model.Compat != nil && model.Compat.SystemOne != nil && model.Compat.SystemOne.ImageInput != nil {
		return *model.Compat.SystemOne.ImageInput
	}
	if model.Provider == "openrouter" || strings.Contains(model.BaseURL, "openrouter.ai") {
		return "state-image-urls"
	}
	return "assets"
}

func appendImageNames(names []string, state json.RawMessage) []any {
	result := make([]any, 0, len(names)+1)
	for _, name := range names {
		result = append(result, name)
	}
	result = append(result, state)
	return result
}

func parseSystemOneAnswers(raw json.RawMessage, questions []ClassifierQuestion) (map[string]ClassifierAnswer, []string, error) {
	answersObject, err := decodeOrderedJSONFields(raw)
	if err != nil {
		return nil, nil, fmt.Errorf("%s returned an unexpected response", systemOneLabel)
	}
	answers := make(map[string]ClassifierAnswer, len(questions))
	order := make([]string, 0, len(questions))
	for _, question := range questions {
		answerRaw, ok := answersObject.raw(question.Name)
		if !ok {
			return nil, nil, fmt.Errorf("%s did not return an answer for %s", systemOneLabel, question.Name)
		}
		answer, err := decodeOrderedJSONFields(answerRaw)
		if err != nil {
			return nil, nil, fmt.Errorf("%s did not return an answer for %s", systemOneLabel, question.Name)
		}
		typeRaw, _ := answer.raw("type")
		answerType, _ := classifierString(typeRaw)
		var parsed ClassifierAnswer
		switch question.Type {
		case "choice":
			if answerType != "choice" {
				return nil, nil, fmt.Errorf("%s did not return a choice answer for %s", systemOneLabel, question.Name)
			}
			parsed.Type = "choice"
			choiceRaw, _ := answer.raw("choice")
			if parsed.Choice, ok = classifierString(choiceRaw); !ok {
				return nil, nil, fmt.Errorf("%s did not return a choice answer for %s", systemOneLabel, question.Name)
			}
			probabilitiesRaw, _ := answer.raw("probabilities")
			probabilities, probabilityOrder, err := parseSystemOneProbabilities(probabilitiesRaw, question.Name)
			if err != nil {
				return nil, nil, err
			}
			parsed.Probabilities, parsed.ProbabilityOrder = probabilities, probabilityOrder
			confidence, err := systemOneNumber(answer, "confidence", "confidence for "+question.Name)
			if err != nil {
				return nil, nil, err
			}
			parsed.Confidence = &confidence
		case "score":
			if answerType != "score" {
				return nil, nil, fmt.Errorf("%s did not return a score answer for %s", systemOneLabel, question.Name)
			}
			parsed.Type = "score"
			score, err := systemOneNumber(answer, "score", "score for "+question.Name)
			if err != nil {
				return nil, nil, err
			}
			confidence, err := systemOneNumber(answer, "confidence", "confidence for "+question.Name)
			if err != nil {
				return nil, nil, err
			}
			parsed.Score, parsed.Confidence = &score, &confidence
		case "bool":
			if answerType != "noul" {
				return nil, nil, fmt.Errorf("%s did not return a bool answer for %s", systemOneLabel, question.Name)
			}
			parsed.Type = "bool"
			probability, err := systemOneNumber(answer, "noul", "probability for "+question.Name)
			if err != nil {
				return nil, nil, err
			}
			parsed.Probability = &probability
		default:
			return nil, nil, fmt.Errorf("%s received invalid question type %q", systemOneLabel, question.Type)
		}
		if _, exists := answers[question.Name]; !exists {
			order = appendJSObjectKey(order, question.Name)
		}
		answers[question.Name] = parsed
	}
	return answers, order, nil
}

func parseSystemOneProbabilities(raw json.RawMessage, question string) (map[string]float64, []string, error) {
	object, err := decodeOrderedJSONFields(raw)
	if err != nil {
		return nil, nil, fmt.Errorf("%s returned invalid probabilities for %s", systemOneLabel, question)
	}
	probabilities := make(map[string]float64, len(object.keys))
	order := make([]string, 0, len(object.keys))
	for _, key := range object.keys {
		value, err := systemOneNumber(object, key, "probability for "+question+"."+key)
		if err != nil {
			return nil, nil, err
		}
		probabilities[key] = value
		order = appendJSObjectKey(order, key)
	}
	return probabilities, order, nil
}

func systemOneNumber(object *orderedJSONFields, key, field string) (float64, error) {
	raw, ok := object.raw(key)
	if !ok {
		return 0, fmt.Errorf("%s returned an invalid %s", systemOneLabel, field)
	}
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return 0, fmt.Errorf("%s returned an invalid %s", systemOneLabel, field)
	}
	number, ok := value.(float64)
	if !ok || math.IsNaN(number) || math.IsInf(number, 0) {
		return 0, fmt.Errorf("%s returned an invalid %s", systemOneLabel, field)
	}
	return number, nil
}

func parseSystemOneUsage(raw json.RawMessage, model *Model) *Usage {
	values, err := decodeOrderedJSONFields(raw)
	if err != nil {
		return nil
	}
	inputRaw, hasInput := values.raw("input_tokens")
	outputRaw, hasOutput := values.raw("output_tokens")
	if !hasInput && !hasOutput {
		return nil
	}
	input := classifierTokenCount(inputRaw)
	output := classifierTokenCount(outputRaw)
	usage := &Usage{Input: input, Output: output, TotalTokens: input + output}
	CalculateCost(model, usage)
	return usage
}
