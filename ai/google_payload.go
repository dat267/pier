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
	contents, err := decodeSchemaNode(encoded)
	if err != nil {
		return "", nil, err
	}
	var body orderedJSONFields
	body.set("contents", contents.fields["contents"])
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
		for _, key := range []string{"temperature", "topP", "topK", "candidateCount", "maxOutputTokens", "stopSequences", "responseLogprobs", "logprobs", "presencePenalty", "frequencyPenalty", "seed", "responseMimeType", "responseJsonSchema", "responseModalities", "mediaResolution", "speechConfig", "thinkingConfig"} {
			if value, ok := payload.Config[key]; ok && strings.TrimSpace(string(value)) != "null" {
				generation.set(key, value)
			}
		}
		if api == APIGoogleVertex {
			for _, key := range []string{"routingConfig", "modelSelectionConfig", "audioTimestamp"} {
				if value, ok := payload.Config[key]; ok && strings.TrimSpace(string(value)) != "null" {
					if key == "modelSelectionConfig" {
						key = "modelConfig"
					}
					generation.set(key, value)
				}
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
