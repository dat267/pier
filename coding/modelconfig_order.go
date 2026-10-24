package coding

import (
	"bytes"
	"encoding/json"
)

func (m *ModelsJSONModel) UnmarshalJSON(data []byte) error {
	type modelAlias ModelsJSONModel
	var decoded modelAlias
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*m = ModelsJSONModel(decoded)
	m.SamplingParamsOrder = modelConfigObjectKeys(modelConfigProperty(data, "samplingParams"))
	m.SamplingParamsByThinkingLevelOrder = modelConfigNestedObjectKeys(modelConfigProperty(data, "samplingParamsByThinkingLevel"))
	return nil
}

func (m *ModelsJSONModelOverride) UnmarshalJSON(data []byte) error {
	type overrideAlias ModelsJSONModelOverride
	var decoded overrideAlias
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*m = ModelsJSONModelOverride(decoded)
	m.SamplingParamsOrder = modelConfigObjectKeys(modelConfigProperty(data, "samplingParams"))
	m.SamplingParamsByThinkingLevelOrder = modelConfigNestedObjectKeys(modelConfigProperty(data, "samplingParamsByThinkingLevel"))
	return nil
}

func modelConfigNestedObjectKeys(raw json.RawMessage) map[string][]string {
	var levels map[string]json.RawMessage
	if len(raw) == 0 || json.Unmarshal(raw, &levels) != nil {
		return nil
	}
	orders := make(map[string][]string, len(levels))
	for level, params := range levels {
		orders[level] = modelConfigObjectKeys(params)
	}
	return orders
}

func modelConfigProperty(data []byte, property string) json.RawMessage {
	dec := json.NewDecoder(bytes.NewReader(data))
	if _, err := dec.Token(); err != nil {
		return nil
	}
	var found json.RawMessage
	for dec.More() {
		token, err := dec.Token()
		if err != nil {
			return nil
		}
		key, ok := token.(string)
		if !ok {
			return nil
		}
		var value json.RawMessage
		if err := dec.Decode(&value); err != nil {
			return nil
		}
		if key == property {
			found = value
		}
	}
	return found
}

func modelConfigRawObjectValues(raw []byte) map[string]json.RawMessage {
	dec := json.NewDecoder(bytes.NewReader(raw))
	if token, err := dec.Token(); err != nil || token != json.Delim('{') {
		return nil
	}
	values := make(map[string]json.RawMessage)
	for dec.More() {
		token, err := dec.Token()
		if err != nil {
			return nil
		}
		key, ok := token.(string)
		if !ok {
			return nil
		}
		var value json.RawMessage
		if err := dec.Decode(&value); err != nil {
			return nil
		}
		values[key] = value
	}
	return values
}

func modelConfigObjectKeys(raw []byte) []string {
	dec := json.NewDecoder(bytes.NewReader(raw))
	if token, err := dec.Token(); err != nil || token != json.Delim('{') {
		return nil
	}
	var keys []string
	for dec.More() {
		token, err := dec.Token()
		if err != nil {
			return nil
		}
		key, ok := token.(string)
		if !ok {
			return nil
		}
		keys = append(keys, key)
		var value json.RawMessage
		if err := dec.Decode(&value); err != nil {
			return nil
		}
	}
	return keys
}
