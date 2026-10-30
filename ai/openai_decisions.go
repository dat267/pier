package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
)

const openAIDecisionsLabel = "OpenAI Decisions"
const openAIDecisionsMaxImages = 128

// OpenAIDecisions implements the first-party OpenAI Decisions classifier API
// (pi v1.4.0 packages/ai/src/api/openai-decisions.ts).
type OpenAIDecisions struct{}

func (OpenAIDecisions) Classify(model *Model, classifierContext ClassifierContext, options *ClassifierOptions) (result *ClassifierResult) {
	if model.API != APIOpenAIDecisions {
		return classifierErrorResult(model, fmt.Errorf("Unsupported classifier API: %s", model.API), options)
	}
	if len(classifierContext.Images) > 0 && !containsString(model.Input, "image") {
		return classifierErrorResult(model, fmt.Errorf("Model %s/%s does not accept image input", model.Provider, model.ID), options)
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
	payload, err := buildOpenAIDecisionsPayload(model, classifierContext)
	if err != nil {
		return classifierFail(result, err, options)
	}
	if options.APIKey == "" {
		return classifierFail(result, fmt.Errorf("No API key for provider: %s", model.Provider), options)
	}
	payloadBytes, err := MarshalJSON(payload)
	if err != nil {
		return classifierFail(result, err, options)
	}
	if options.OnPayload != nil {
		if transformed := options.OnPayload(payloadBytes, model); transformed != nil {
			payloadBytes, err = MarshalJSON(transformed)
			if err != nil {
				return classifierFail(result, err, options)
			}
		}
	}
	ctx := options.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	maxRetries := 2
	if options.MaxRetries != nil {
		maxRetries = *options.MaxRetries
	}
	response, err := RetryProviderRequest(ctx, func() (*http.Response, error) {
		request, err := http.NewRequestWithContext(ctx, http.MethodPost,
			strings.TrimRight(model.BaseURL, "/")+"/decisions", bytes.NewReader(payloadBytes))
		if err != nil {
			return nil, err
		}
		request.Header.Set("Content-Type", "application/json")
		ApplyIdentityHeaders(request.Header, model, options.Identity)
		request.Header.Set("Authorization", "Bearer "+options.APIKey)
		for name, value := range model.Headers {
			request.Header.Set(name, value)
		}
		applyProviderHeaders(request.Header, options.Headers)
		response, err := DoProviderRequestWithAbortSignalTimeoutAndIdle(request, options.TimeoutMs, options.HTTPIdleTimeoutMs)
		if err != nil {
			return nil, err
		}
		if response.StatusCode >= 200 && response.StatusCode < 300 {
			return response, nil
		}
		body, readErr := io.ReadAll(io.LimitReader(response.Body, 1<<20))
		_ = response.Body.Close()
		if readErr != nil {
			return nil, readErr
		}
		headers := response.Header.Clone()
		if response.StatusCode == http.StatusGatewayTimeout {
			headers.Set("x-should-retry", "false")
		}
		return nil, &ProviderError{
			Status: response.StatusCode, Headers: headers,
			Message: fmt.Sprintf("%s returned %d", openAIDecisionsLabel, response.StatusCode), Body: string(body),
		}
	}, &ProviderRetryOptions{MaxRetries: &maxRetries, MaxRetryDelayMS: options.MaxRetryDelayMs})
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return classifierFail(result, err, options)
		}
		if errors.Is(err, context.DeadlineExceeded) && ctx.Err() == nil && options.TimeoutMs != nil {
			return classifierFail(result, fmt.Errorf("Request timed out after %dms", *options.TimeoutMs), options)
		}
		return classifierFail(result, openAIDecisionsError(err), options)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	if err != nil {
		return classifierFail(result, err, options)
	}
	var body struct {
		Answers json.RawMessage `json:"answers"`
		Usage   json.RawMessage `json:"usage"`
	}
	if err := json.Unmarshal(data, &body); err != nil {
		return classifierFail(result, fmt.Errorf("%s returned an unexpected response", openAIDecisionsLabel), options)
	}
	result.Usage = parseOpenAIDecisionsUsage(body.Usage, model)
	if options.OnResponse != nil {
		options.OnResponse(ProviderResponse{Status: response.StatusCode, Headers: responseHeaders(response.Header)}, model)
	}
	answers, answerOrder, err := parseOpenAIDecisionsAnswers(body.Answers, classifierContext.Questions)
	if err != nil {
		return classifierFail(result, err, options)
	}
	result.Answers = answers
	result.AnswerOrder = answerOrder
	return result
}

func classifierFail(result *ClassifierResult, err error, options *ClassifierOptions) *ClassifierResult {
	result.Answers = map[string]ClassifierAnswer{}
	result.AnswerOrder = nil
	result.StopReason = ClassifierStopError
	if options != nil && options.Ctx != nil && options.Ctx.Err() != nil {
		result.StopReason = ClassifierStopAborted
	}
	message := err.Error()
	result.ErrorMessage = &message
	return result
}

func openAIDecisionsError(err error) error {
	var providerError *ProviderError
	if errors.As(err, &providerError) && providerError.Status == http.StatusGatewayTimeout {
		return fmt.Errorf("%s error (504): the request timed out at the gateway. Very large inputs (above roughly 600K tokens) currently exceed its time limit.", openAIDecisionsLabel)
	}
	return fmt.Errorf("%s", FormatProviderError(NormalizeProviderError(err), openAIDecisionsLabel+" error"))
}

type openAIDecisionsPayload struct {
	Model     string                    `json:"model"`
	Input     any                       `json:"input"`
	Questions []openAIDecisionsQuestion `json:"questions"`
}

type openAIDecisionsQuestion struct {
	Type         string                   `json:"type"`
	Name         string                   `json:"name"`
	Instructions string                   `json:"instructions"`
	Choices      *[]openAIDecisionsChoice `json:"choices,omitempty"`
	Levels       *[]openAIDecisionsLevel  `json:"levels,omitempty"`
}

type openAIDecisionsChoice struct {
	Value       string `json:"value"`
	Description string `json:"description,omitempty"`
}

type openAIDecisionsLevel struct {
	Label string `json:"label"`
}

type openAIDecisionsInputMessage struct {
	Role    string                        `json:"role"`
	Content []openAIDecisionsInputContent `json:"content"`
}

type openAIDecisionsInputContent struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	ImageURL string `json:"image_url,omitempty"`
}

func buildOpenAIDecisionsPayload(model *Model, context ClassifierContext) (openAIDecisionsPayload, error) {
	if len(context.Images) > openAIDecisionsMaxImages {
		return openAIDecisionsPayload{}, fmt.Errorf("%s accepts at most %d images, got %d", openAIDecisionsLabel, openAIDecisionsMaxImages, len(context.Images))
	}
	state := context.State
	if len(state) == 0 {
		state = json.RawMessage("{}")
	}
	stateJSON, err := MarshalJSON(state)
	if err != nil {
		return openAIDecisionsPayload{}, fmt.Errorf("%s state must be valid JSON", openAIDecisionsLabel)
	}
	input := any(string(stateJSON))
	if len(context.Images) > 0 {
		content := make([]openAIDecisionsInputContent, 0, len(context.Images)+1)
		content = append(content, openAIDecisionsInputContent{Type: "input_text", Text: string(stateJSON)})
		for _, image := range context.Images {
			content = append(content, openAIDecisionsInputContent{
				Type: "input_image", ImageURL: "data:" + image.MimeType + ";base64," + image.Data,
			})
		}
		input = []openAIDecisionsInputMessage{{Role: "user", Content: content}}
	}
	questions := make([]openAIDecisionsQuestion, 0, len(context.Questions))
	for _, question := range context.Questions {
		wire := openAIDecisionsQuestion{Name: question.Name, Instructions: question.Instructions}
		switch question.Type {
		case "choice":
			wire.Type = "choice"
			choices := make([]openAIDecisionsChoice, 0, len(question.Choices))
			for _, choice := range question.Choices {
				choices = append(choices, openAIDecisionsChoice{Value: choice.Value, Description: choice.Description})
			}
			wire.Choices = &choices
		case "score":
			wire.Type = "score"
			levels := make([]openAIDecisionsLevel, 0, len(question.Levels))
			for _, label := range question.Levels {
				levels = append(levels, openAIDecisionsLevel{Label: label})
			}
			wire.Levels = &levels
		case "bool":
			wire.Type = "predicate"
			var meanings []string
			if question.BoolCriteria.True != "" {
				meanings = append(meanings, "True means: "+question.BoolCriteria.True)
			}
			if question.BoolCriteria.False != "" {
				meanings = append(meanings, "False means: "+question.BoolCriteria.False)
			}
			if len(meanings) > 0 {
				wire.Instructions += "\n\n" + strings.Join(meanings, "\n")
			}
		default:
			return openAIDecisionsPayload{}, fmt.Errorf("%s received invalid question type %q", openAIDecisionsLabel, question.Type)
		}
		questions = append(questions, wire)
	}
	return openAIDecisionsPayload{Model: model.ID, Input: input, Questions: questions}, nil
}

func parseOpenAIDecisionsAnswers(raw json.RawMessage, questions []ClassifierQuestion) (map[string]ClassifierAnswer, []string, error) {
	var entries []json.RawMessage
	if json.Unmarshal(raw, &entries) != nil || entries == nil {
		return nil, nil, fmt.Errorf("%s returned an unexpected response", openAIDecisionsLabel)
	}
	byName := make(map[string]map[string]json.RawMessage, len(entries))
	for _, rawEntry := range entries {
		var entry map[string]json.RawMessage
		if json.Unmarshal(rawEntry, &entry) != nil || entry == nil {
			continue
		}
		if name, ok := classifierString(entry["name"]); ok {
			byName[name] = entry
		}
	}
	answers := make(map[string]ClassifierAnswer, len(questions))
	answerOrder := make([]string, 0, len(questions))
	for _, question := range questions {
		answer, ok := byName[question.Name]
		if !ok {
			return nil, nil, fmt.Errorf("%s did not return an answer for %s", openAIDecisionsLabel, question.Name)
		}
		var answerType string
		_ = json.Unmarshal(answer["type"], &answerType)
		if answerType == "refusal" {
			return nil, nil, fmt.Errorf("%s refused to answer %s", openAIDecisionsLabel, question.Name)
		}
		var parsed ClassifierAnswer
		switch question.Type {
		case "choice":
			if answerType != "choice" {
				return nil, nil, fmt.Errorf("%s did not return a choice answer for %s", openAIDecisionsLabel, question.Name)
			}
			parsed.Type = "choice"
			var ok bool
			if parsed.Choice, ok = classifierString(answer["choice"]); !ok {
				return nil, nil, fmt.Errorf("%s did not return a choice answer for %s", openAIDecisionsLabel, question.Name)
			}
			probabilities, order, err := parseOpenAIDecisionsProbabilities(answer["probabilities"], question.Name)
			if err != nil {
				return nil, nil, err
			}
			parsed.Probabilities = probabilities
			parsed.ProbabilityOrder = order
			confidence, err := classifierNumber(answer["confidence"], "confidence for "+question.Name)
			if err != nil {
				return nil, nil, err
			}
			parsed.Confidence = &confidence
		case "score":
			if answerType != "score" {
				return nil, nil, fmt.Errorf("%s did not return a score answer for %s", openAIDecisionsLabel, question.Name)
			}
			parsed.Type = "score"
			score, err := classifierNumber(answer["score"], "score for "+question.Name)
			if err != nil {
				return nil, nil, err
			}
			confidence, err := classifierNumber(answer["confidence"], "confidence for "+question.Name)
			if err != nil {
				return nil, nil, err
			}
			parsed.Score, parsed.Confidence = &score, &confidence
		case "bool":
			if answerType != "predicate" {
				return nil, nil, fmt.Errorf("%s did not return a predicate answer for %s", openAIDecisionsLabel, question.Name)
			}
			parsed.Type = "bool"
			probability, err := classifierNumber(answer["probability"], "probability for "+question.Name)
			if err != nil {
				return nil, nil, err
			}
			parsed.Probability = &probability
		}
		if _, exists := answers[question.Name]; !exists {
			answerOrder = appendJSObjectKey(answerOrder, question.Name)
		}
		answers[question.Name] = parsed
	}
	return answers, answerOrder, nil
}

func parseOpenAIDecisionsProbabilities(raw json.RawMessage, id string) (map[string]float64, []string, error) {
	var values []struct {
		Value       json.RawMessage `json:"value"`
		Probability json.RawMessage `json:"probability"`
	}
	if json.Unmarshal(raw, &values) != nil || values == nil {
		return nil, nil, fmt.Errorf("%s returned invalid probabilities for %s", openAIDecisionsLabel, id)
	}
	probabilities := make(map[string]float64, len(values))
	order := make([]string, 0, len(values))
	for _, value := range values {
		name, ok := classifierString(value.Value)
		if !ok {
			return nil, nil, fmt.Errorf("%s returned invalid probabilities for %s", openAIDecisionsLabel, id)
		}
		probability, err := classifierNumber(value.Probability, "probability for "+id+"."+name)
		if err != nil {
			return nil, nil, err
		}
		if _, exists := probabilities[name]; !exists {
			order = appendJSObjectKey(order, name)
		}
		probabilities[name] = probability
	}
	return probabilities, order, nil
}

func classifierString(raw json.RawMessage) (string, bool) {
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return "", false
	}
	text, ok := value.(string)
	return text, ok
}

func classifierNumber(raw json.RawMessage, field string) (float64, error) {
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return 0, fmt.Errorf("%s returned an invalid %s", openAIDecisionsLabel, field)
	}
	number, ok := value.(float64)
	if !ok || math.IsNaN(number) || math.IsInf(number, 0) {
		return 0, fmt.Errorf("%s returned an invalid %s", openAIDecisionsLabel, field)
	}
	return number, nil
}

func parseOpenAIDecisionsUsage(raw json.RawMessage, model *Model) *Usage {
	var values map[string]json.RawMessage
	if json.Unmarshal(raw, &values) != nil || values == nil || (values["input_tokens"] == nil && values["output_tokens"] == nil) {
		return nil
	}
	input := classifierTokenCount(values["input_tokens"])
	output := classifierTokenCount(values["output_tokens"])
	usage := &Usage{Input: input, Output: output, TotalTokens: input + output}
	CalculateCost(model, usage)
	return usage
}

func classifierTokenCount(raw json.RawMessage) int64 {
	value, err := classifierNumber(raw, "token count")
	if err != nil || value <= 0 || value >= math.MaxInt64 {
		return 0
	}
	return int64(value)
}

func responseHeaders(headers http.Header) map[string]string {
	out := make(map[string]string, len(headers))
	for name, values := range headers {
		if len(values) > 0 {
			out[strings.ToLower(name)] = strings.Join(values, ", ")
		}
	}
	return out
}
