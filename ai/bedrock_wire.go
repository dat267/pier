package ai

import (
	"encoding/json"
	"strings"
)

type bedrockWireField struct {
	name  string
	shape string
}

func marshalBedrockRequestMetadata(values map[string]string, preferred []string) (json.RawMessage, error) {
	var fields orderedJSONFields
	seen := make(map[string]bool, len(values))
	keys := append([]string(nil), preferred...)
	remaining := make([]string, 0, len(values))
	for key := range values {
		if !containsString(keys, key) {
			remaining = append(remaining, key)
		}
	}
	sortStrings(remaining)
	keys = append(keys, remaining...)
	for _, key := range keys {
		value, ok := values[key]
		if !ok || seen[key] {
			continue
		}
		seen[key] = true
		if err := fields.setAny(key, value); err != nil {
			return nil, err
		}
	}
	encoded, err := fields.marshal()
	return json.RawMessage(encoded), err
}

// These fields are a provider Document, so the SDK preserves the order in
// which buildAdditionalModelRequestFields constructs them instead of applying
// a modeled-structure field order.
func marshalBedrockAdditionalFields(input map[string]any) ([]byte, error) {
	var fields orderedJSONFields
	for _, key := range []string{"thinking", "output_config", "anthropic_beta", "reasoning_effort", "reasoning"} {
		value, exists := input[key]
		if !exists {
			continue
		}
		if key == "thinking" {
			var thinking orderedJSONFields
			for _, name := range []string{"type", "budget_tokens", "display", "block_binding"} {
				if nested, ok := value.(map[string]any)[name]; ok {
					if err := thinking.setAny(name, nested); err != nil {
						return nil, err
					}
				}
			}
			encoded, err := thinking.marshal()
			if err != nil {
				return nil, err
			}
			fields.set(key, encoded)
		} else if err := fields.setAny(key, value); err != nil {
			return nil, err
		}
	}
	return fields.marshal()
}

// Bedrock sends a Smithy command through the SDK's modeled JSON serializer.
// Its HTTP body excludes path labels and unknown fields; Document values retain
// their own order. The Go adapter uses the generated request-only shape graph.
// Reference: pi v1.4.0 api/bedrock-converse-stream.ts, ConverseStreamCommand.
func marshalBedrockCommand(input any, hook bool) ([]byte, error) {
	encoded, err := MarshalJSON(input)
	if err != nil {
		return nil, err
	}
	return projectBedrockJSON(encoded, "ConverseStreamRequest", hook)
}

func projectBedrockJSON(raw json.RawMessage, shape string, hook bool) (json.RawMessage, error) {
	if shape == "" {
		return raw, nil
	}
	if strings.HasPrefix(shape, "[]") {
		var list []json.RawMessage
		if err := jsonUnmarshalStrict(raw, &list); err != nil {
			return nil, err
		}
		for i, item := range list {
			encoded, err := projectBedrockJSON(item, shape[2:], hook)
			if err != nil {
				return nil, err
			}
			list[i] = encoded
		}
		return MarshalJSON(list)
	}
	node, err := decodeOrderedJSONFields(raw)
	if err != nil {
		return nil, err
	}
	var fields orderedJSONFields
	if strings.HasPrefix(shape, "{}") {
		for _, key := range node.keys {
			value, err := projectBedrockJSON(node.values[key], shape[2:], hook)
			if err != nil {
				return nil, err
			}
			fields.set(key, value)
		}
		return fields.marshal()
	}
	members := bedrockWireShapes[shape]
	if hook {
		switch shape {
		case "ConverseStreamRequest":
			members = append([]bedrockWireField{{"modelId", ""}}, members...)
		case "ToolSpecification":
			members = []bedrockWireField{{"name", ""}, {"description", ""}, {"inputSchema", "ToolInputSchema"}, {"strict", ""}}
		case "ImageBlock":
			members = []bedrockWireField{{"source", "ImageSource"}, {"format", ""}}
		}
	}
	for _, member := range members {
		value, exists := node.values[member.name]
		if !exists || string(value) == "null" {
			continue
		}
		encoded, err := projectBedrockJSON(value, member.shape, hook)
		if err != nil {
			return nil, err
		}
		fields.set(member.name, encoded)
	}
	return fields.marshal()
}
