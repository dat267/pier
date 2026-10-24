package ai

import (
	"encoding/json"
	"fmt"
	"strings"
)

// googleSDKPayload is the pre-serialization parameter object exposed by pi's
// Google hooks, not the REST body. Reference: pi v1.4.0 (4a080252),
// api/google-generative-ai.ts and api/google-vertex.ts buildParams.
func googleSDKPayload(model *Model, params *GoogleGenerateContentParams) ([]byte, error) {
	var config orderedJSONFields
	set := func(key string, value any) error { return config.setAny(key, value) }
	if generation := params.GenerationConfig; generation != nil {
		if generation.Temperature != nil {
			if err := set("temperature", generation.Temperature); err != nil {
				return nil, err
			}
		}
		if generation.MaxOutputTokens != nil {
			if err := set("maxOutputTokens", generation.MaxOutputTokens); err != nil {
				return nil, err
			}
		}
	}
	if params.SystemInstruction != nil && len(params.SystemInstruction.Parts) > 0 {
		if err := set("systemInstruction", params.SystemInstruction.Parts[0].Text); err != nil {
			return nil, err
		}
	}
	if len(params.Tools) > 0 {
		if err := set("tools", params.Tools); err != nil {
			return nil, err
		}
	}
	if params.ToolConfig != nil {
		if err := set("toolConfig", params.ToolConfig); err != nil {
			return nil, err
		}
	}
	if generation := params.GenerationConfig; generation != nil && generation.ThinkingConfig != nil {
		if err := set("thinkingConfig", generation.ThinkingConfig); err != nil {
			return nil, err
		}
	}
	encodedConfig, err := config.marshal()
	if err != nil {
		return nil, err
	}
	type content struct {
		Role  string          `json:"role"`
		Parts []googleSDKPart `json:"parts"`
	}
	contents := make([]content, len(params.Contents))
	for i, original := range params.Contents {
		parts := make([]googleSDKPart, len(original.Parts))
		for j, part := range original.Parts {
			parts[j] = googleSDKPart(part)
		}
		contents[i] = content{Role: original.Role, Parts: parts}
	}
	var payload orderedJSONFields
	if err := payload.setAny("model", model.ID); err != nil {
		return nil, err
	}
	if err := payload.setAny("contents", contents); err != nil {
		return nil, err
	}
	payload.set("config", encodedConfig)
	return payload.marshal()
}

type googleSDKPart GooglePart

func (p googleSDKPart) MarshalJSON() ([]byte, error) {
	if p.Thought != nil {
		return MarshalJSON(struct {
			Thought          *bool   `json:"thought"`
			Text             string  `json:"text"`
			ThoughtSignature *string `json:"thoughtSignature,omitempty"`
		}{p.Thought, p.Text, p.ThoughtSignature})
	}
	return MarshalJSON(GooglePart(p))
}

// googleVertexOnlyReplacements are the config members the Developer API
// serializer rejects before it reaches modelArmorConfig (SDK
// generateContentConfigToMldev; imageConfig sits between the two).
var googleVertexOnlyReplacements = []string{"routingConfig", "modelSelectionConfig", "labels", "audioTimestamp"}

const (
	// googleModelArmorReplacement is rejected last by the Developer serializer.
	googleModelArmorReplacement = "modelArmorConfig"
	// googleCivicAnswersReplacement is the only member the Vertex serializer
	// rejects (SDK generateContentConfigToVertex).
	googleCivicAnswersReplacement = "enableEnhancedCivicAnswers"
)

func googleReplacementError(key string, api Api) error {
	if api == APIGoogleGenerativeAI {
		return fmt.Errorf("%s parameter is only supported in Gemini Enterprise Agent Platform mode, not in Gemini Developer API mode.", key)
	}
	return fmt.Errorf("%s parameter is only supported in Gemini Developer API mode, not in Gemini Enterprise Agent Platform mode.", key)
}

// googleValidateReplacements reproduces the SDK's per-API config validation,
// which rejects members the other API supports. The hook has already run by
// the time the SDK serializes, so these surface before any request is sent.
func googleValidateReplacements(config map[string]json.RawMessage, api Api) error {
	if api == APIGoogleGenerativeAI {
		for _, key := range googleVertexOnlyReplacements {
			if _, ok := config[key]; ok {
				return googleReplacementError(key, api)
			}
		}
		if raw, ok := config["imageConfig"]; ok && strings.TrimSpace(string(raw)) != "null" {
			if _, err := googleDeveloperImageConfig(raw); err != nil {
				return err
			}
		}
		if _, ok := config[googleModelArmorReplacement]; ok {
			return googleReplacementError(googleModelArmorReplacement, api)
		}
		return nil
	}
	if _, ok := config[googleCivicAnswersReplacement]; ok {
		return googleReplacementError(googleCivicAnswersReplacement, api)
	}
	return nil
}

// googleDeveloperImageConfig maps a replacement imageConfig the way the
// Developer API serializer does: only aspectRatio and imageSize survive, and
// the Vertex-only members are rejected when present.
func googleDeveloperImageConfig(raw json.RawMessage) (json.RawMessage, error) {
	source, err := decodeOrderedJSONFields(raw)
	if err != nil {
		return nil, err
	}
	var config orderedJSONFields
	for _, key := range []string{"aspectRatio", "imageSize"} {
		if value, ok := source.raw(key); ok && strings.TrimSpace(string(value)) != "null" {
			config.set(key, value)
		}
	}
	for _, key := range []string{"personGeneration", "outputMimeType", "outputCompressionQuality", "imageOutputOptions", "prominentPeople"} {
		if _, ok := source.raw(key); ok {
			return nil, googleReplacementError(key, APIGoogleGenerativeAI)
		}
	}
	return config.marshal()
}

// googleSDKRequest applies a hook's SDK-shaped replacement before the REST
// serializer. Generation controls use the SDK's field order, not hook order.
func googleSDKRequest(raw json.RawMessage, api Api) (string, []byte, error) {
	var payload struct {
		Model    string                     `json:"model"`
		Contents []GoogleContent            `json:"contents"`
		Config   map[string]json.RawMessage `json:"config"`
	}
	if err := jsonUnmarshalStrict(raw, &payload); err != nil {
		return "", nil, err
	}
	if payload.Model == "" {
		return "", nil, fmt.Errorf("model is required")
	}
	if err := googleValidateReplacements(payload.Config, api); err != nil {
		return "", nil, err
	}
	params := &GoogleGenerateContentParams{Contents: payload.Contents}
	var encoded []byte
	var err error
	if api == APIGoogleGenerativeAI {
		encoded, err = marshalGoogleDeveloperRequest(params)
	} else {
		encoded, err = MarshalJSON(params)
	}
	if err != nil {
		return "", nil, err
	}
	contents, err := decodeOrderedJSONFields(encoded)
	if err != nil {
		return "", nil, err
	}
	var body orderedJSONFields
	body.set("contents", contents.values["contents"])
	if value, ok := payload.Config["serviceTier"]; ok && strings.TrimSpace(string(value)) != "null" {
		body.set("serviceTier", value)
	}
	if system, ok := payload.Config["systemInstruction"]; ok && strings.TrimSpace(string(system)) != "null" {
		var text string
		if jsonUnmarshalStrict(system, &text) == nil {
			system, err = MarshalJSON(GoogleSystemInstruction{Parts: []GooglePart{{Text: text}}, Role: "user"})
		} else {
			var content GoogleSystemInstruction
			if err = jsonUnmarshalStrict(system, &content); err == nil {
				system, err = MarshalJSON(content)
			}
		}
		if err != nil {
			return "", nil, err
		}
		body.set("systemInstruction", system)
	}
	for _, key := range []string{"tools", "toolConfig", "safetySettings", "cachedContent"} {
		if value, ok := payload.Config[key]; ok && strings.TrimSpace(string(value)) != "null" {
			body.set(key, value)
		}
	}
	if api == APIGoogleVertex {
		for _, key := range []string{"labels", "modelArmorConfig"} {
			if value, ok := payload.Config[key]; ok && strings.TrimSpace(string(value)) != "null" {
				body.set(key, value)
			}
		}
	}
	if payload.Config != nil {
		var generation orderedJSONFields
		if api == APIGoogleVertex {
			for _, key := range []string{"routingConfig", "modelSelectionConfig"} {
				if value, ok := payload.Config[key]; ok && strings.TrimSpace(string(value)) != "null" {
					if key == "modelSelectionConfig" {
						key = "modelConfig"
					}
					generation.set(key, value)
				}
			}
		}
		generationKeys := []string{"temperature", "topP", "topK", "candidateCount", "maxOutputTokens", "stopSequences", "responseLogprobs", "logprobs", "presencePenalty", "frequencyPenalty", "seed", "responseMimeType", "responseSchema", "responseJsonSchema", "responseModalities", "mediaResolution", "speechConfig"}
		if api == APIGoogleVertex {
			generationKeys = append(generationKeys, "audioTimestamp")
		}
		generationKeys = append(generationKeys, "thinkingConfig", "audioTranscriptionConfig", "imageConfig")
		for _, key := range generationKeys {
			if value, ok := payload.Config[key]; ok && strings.TrimSpace(string(value)) != "null" {
				if key == "imageConfig" {
					if api == APIGoogleVertex {
						value, err = googleVertexImageConfig(value)
					} else {
						value, err = googleDeveloperImageConfig(value)
					}
					if err != nil {
						return "", nil, err
					}
				}
				generation.set(key, value)
			}
		}
		if api == APIGoogleGenerativeAI {
			if value, ok := payload.Config["enableEnhancedCivicAnswers"]; ok && strings.TrimSpace(string(value)) != "null" {
				generation.set("enableEnhancedCivicAnswers", value)
			}
		}
		generationJSON, err := generation.marshal()
		if err != nil {
			return "", nil, err
		}
		body.set("generationConfig", generationJSON)
	}
	bodyJSON, err := body.marshal()
	if err != nil {
		return "", nil, err
	}
	bodyJSON, err = MarshalJSON(json.RawMessage(bodyJSON))
	return payload.Model, bodyJSON, err
}

func googleVertexImageConfig(raw json.RawMessage) (json.RawMessage, error) {
	source, err := decodeOrderedJSONFields(raw)
	if err != nil {
		return nil, err
	}
	var config orderedJSONFields
	for _, key := range []string{"aspectRatio", "imageSize", "personGeneration"} {
		if value, ok := source.raw(key); ok && strings.TrimSpace(string(value)) != "null" {
			config.set(key, value)
		}
	}
	if options, ok := source.raw("imageOutputOptions"); ok && strings.TrimSpace(string(options)) != "null" {
		config.set("imageOutputOptions", options)
	} else {
		var output orderedJSONFields
		if value, ok := source.raw("outputMimeType"); ok && strings.TrimSpace(string(value)) != "null" {
			output.set("mimeType", value)
		}
		if value, ok := source.raw("outputCompressionQuality"); ok && strings.TrimSpace(string(value)) != "null" {
			output.set("compressionQuality", value)
		}
		if len(output.keys) > 0 {
			encoded, err := output.marshal()
			if err != nil {
				return nil, err
			}
			config.set("imageOutputOptions", encoded)
		}
	}
	if value, ok := source.raw("prominentPeople"); ok && strings.TrimSpace(string(value)) != "null" {
		config.set("prominentPeople", value)
	}
	return config.marshal()
}
