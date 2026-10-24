package coding

import (
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
	keys, values, err := readJSONObjectOrdered(data)
	if err != nil {
		return nil
	}
	for _, key := range keys {
		if key == property {
			return values[key]
		}
	}
	return nil
}

func modelConfigRawObjectValues(raw []byte) map[string]json.RawMessage {
	_, values, err := readJSONObjectOrdered(raw)
	if err != nil {
		return nil
	}
	return values
}

func modelConfigObjectKeys(raw []byte) []string {
	keys, _, err := readJSONObjectOrdered(raw)
	if err != nil {
		return nil
	}
	return keys
}
