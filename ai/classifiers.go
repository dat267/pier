package ai

import (
	"encoding/json"
	"sort"
	"time"
)

// ClassifierQuestion is one named structured classification request. Exactly
// one criteria arm is used by each question type (upstream types.ts).
type ClassifierQuestion struct {
	Name         string
	Type         string
	Instructions string
	Choices      []ClassifierChoice
	Levels       []string
	BoolCriteria ClassifierBoolCriteria
}

// ClassifierChoice is one choice value and its optional description.
type ClassifierChoice struct {
	Value       string
	Description string
}

// ClassifierBoolCriteria explains the true and false outcomes.
type ClassifierBoolCriteria struct {
	True  string
	False string
}

// ClassifierContext contains the JSON state, optional images, and ordered
// questions to classify. State is raw JSON so callers can preserve object-key
// order when reproducing JavaScript JSON.stringify behavior.
type ClassifierContext struct {
	State     json.RawMessage
	Images    []ImageContent
	Questions []ClassifierQuestion
}

// ClassifierOptions are the request options shared by classifier APIs.
type ClassifierOptions struct {
	StreamOptions
}

// ClassifierAnswer is one parsed answer. Fields irrelevant to Type are omitted.
type ClassifierAnswer struct {
	Type             string             `json:"type"`
	Choice           string             `json:"choice,omitempty"`
	Probabilities    map[string]float64 `json:"probabilities,omitempty"`
	ProbabilityOrder []string           `json:"-"`
	Confidence       *float64           `json:"confidence,omitempty"`
	Score            *float64           `json:"score,omitempty"`
	Probability      *float64           `json:"probability,omitempty"`
}

// MarshalJSON preserves the question-specific field order used by pi.
func (answer ClassifierAnswer) MarshalJSON() ([]byte, error) {
	values := map[string]any{"type": answer.Type}
	keys := []string{"type"}
	switch answer.Type {
	case "choice":
		values["choice"] = answer.Choice
		keys = append(keys, "choice")
		probabilities := answer.Probabilities
		if probabilities == nil {
			probabilities = map[string]float64{}
		}
		values["probabilities"] = orderedClassifierProbabilities(probabilities, answer.ProbabilityOrder)
		keys = append(keys, "probabilities")
		if answer.Confidence != nil {
			values["confidence"] = *answer.Confidence
			keys = append(keys, "confidence")
		}
	case "score":
		if answer.Score != nil {
			values["score"] = *answer.Score
			keys = append(keys, "score")
		}
		if answer.Confidence != nil {
			values["confidence"] = *answer.Confidence
			keys = append(keys, "confidence")
		}
	case "bool":
		if answer.Probability != nil {
			values["probability"] = *answer.Probability
			keys = append(keys, "probability")
		}
	}
	return (&orderedMap{keys: keys, values: values}).MarshalJSON()
}

func orderedClassifierProbabilities(values map[string]float64, order []string) *orderedMap {
	keys := append([]string(nil), order...)
	var extras []string
	for key := range values {
		if !containsString(keys, key) {
			extras = append(extras, key)
		}
	}
	sort.Strings(extras)
	for _, key := range extras {
		keys = appendJSObjectKey(keys, key)
	}
	ordered := &orderedMap{keys: keys, values: make(map[string]any, len(values))}
	for key, value := range values {
		ordered.values[key] = value
	}
	return ordered
}

// ClassifierResult is the non-rejecting result of a classifier request.
type ClassifierResult struct {
	API          string                      `json:"api"`
	Provider     ProviderId                  `json:"provider"`
	Model        string                      `json:"model"`
	Answers      map[string]ClassifierAnswer `json:"answers"`
	AnswerOrder  []string                    `json:"-"`
	Usage        *Usage                      `json:"usage,omitempty"`
	StopReason   string                      `json:"stopReason"`
	ErrorMessage *string                     `json:"errorMessage,omitempty"`
	Timestamp    int64                       `json:"timestamp"`
}

// MarshalJSON retains upstream insertion order, including fields appended to
// results after their initial construction.
func (result ClassifierResult) MarshalJSON() ([]byte, error) {
	values := map[string]any{
		"api": result.API, "provider": result.Provider, "model": result.Model,
		"answers":    orderedClassifierAnswers(result.Answers, result.AnswerOrder),
		"stopReason": result.StopReason, "timestamp": result.Timestamp,
	}
	keys := []string{"api", "provider", "model", "answers", "stopReason", "timestamp"}
	if result.Usage != nil {
		values["usage"] = result.Usage
		keys = append(keys, "usage")
	}
	if result.ErrorMessage != nil {
		values["errorMessage"] = *result.ErrorMessage
		keys = append(keys, "errorMessage")
	}
	return (&orderedMap{keys: keys, values: values}).MarshalJSON()
}

func orderedClassifierAnswers(values map[string]ClassifierAnswer, order []string) *orderedMap {
	keys := append([]string(nil), order...)
	var extras []string
	for key := range values {
		if !containsString(keys, key) {
			extras = append(extras, key)
		}
	}
	sort.Strings(extras)
	for _, key := range extras {
		keys = appendJSObjectKey(keys, key)
	}
	ordered := &orderedMap{keys: keys, values: make(map[string]any, len(values))}
	for key, value := range values {
		ordered.values[key] = value
	}
	return ordered
}

// ClassifierStopReason labels a classifier result.
type ClassifierStopReason = string

const (
	ClassifierStopStop    ClassifierStopReason = "stop"
	ClassifierStopError   ClassifierStopReason = "error"
	ClassifierStopAborted ClassifierStopReason = "aborted"
)

// ProviderClassifier is the uniform structured-classification API contract.
type ProviderClassifier interface {
	Classify(model *Model, context ClassifierContext, options *ClassifierOptions) *ClassifierResult
}

func classifierErrorResult(model *Model, err error, options *ClassifierOptions) *ClassifierResult {
	if model == nil {
		model = &Model{}
	}
	message := "unknown classifier error"
	if err != nil {
		message = err.Error()
	}
	stopReason := ClassifierStopError
	if options != nil && options.Ctx != nil && options.Ctx.Err() != nil {
		stopReason = ClassifierStopAborted
	}
	return &ClassifierResult{
		API: model.API, Provider: model.Provider, Model: model.ID,
		Answers: map[string]ClassifierAnswer{}, StopReason: stopReason,
		ErrorMessage: &message, Timestamp: time.Now().UnixMilli(),
	}
}
