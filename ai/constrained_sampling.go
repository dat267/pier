package ai

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Port of api/constrained-sampling.ts.

type strictSchemaError struct{ msg string }

func (e *strictSchemaError) Error() string { return e.msg }

var unsupportedStrictSchemaKeys = []string{
	"$ref", "$defs", "definitions", "allOf", "oneOf", "patternProperties",
	"dependentSchemas", "dependencies", "unevaluatedProperties", "propertyNames",
	"contains", "prefixItems", "not", "if", "then", "else",
}

// schemaNode is a decoded JSON-schema object preserving key order for
// property maps (Go maps would sort; use ordered maps).
type schemaNode struct {
	fields map[string]json.RawMessage
	keys   []string
}

func decodeSchemaNode(data []byte) (*schemaNode, error) {
	dec := json.NewDecoder(strings.NewReader(string(data)))
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	if delim, ok := tok.(json.Delim); !ok || delim != '{' {
		return nil, fmt.Errorf("not an object")
	}
	node := &schemaNode{fields: map[string]json.RawMessage{}}
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		key := tok.(string)
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return nil, err
		}
		if _, seen := node.fields[key]; !seen {
			node.keys = append(node.keys, key)
		}
		node.fields[key] = raw
	}
	return node, nil
}

func (n *schemaNode) encode() ([]byte, error) {
	var buf []byte
	buf = append(buf, '{')
	for i, key := range n.keys {
		if i > 0 {
			buf = append(buf, ',')
		}
		keyEnc, _ := MarshalJSON(key)
		buf = append(buf, keyEnc...)
		buf = append(buf, ':')
		buf = append(buf, n.fields[key]...)
	}
	return append(buf, '}'), nil
}

func (n *schemaNode) raw(key string) (json.RawMessage, bool) {
	v, ok := n.fields[key]
	return v, ok
}

func (n *schemaNode) set(key string, value json.RawMessage) {
	if _, seen := n.fields[key]; !seen {
		n.keys = append(n.keys, key)
	}
	n.fields[key] = value
}

func schemaTypes(data json.RawMessage) []string {
	var single string
	if err := jsonUnmarshalStrict(data, &single); err == nil {
		return []string{single}
	}
	var list []string
	if err := jsonUnmarshalStrict(data, &list); err == nil {
		return list
	}
	return nil
}

func isStructuredSchema(data json.RawMessage) bool {
	node, err := decodeSchemaNode(data)
	if err != nil {
		return false
	}
	types := []string{}
	if t, ok := node.raw("type"); ok {
		types = schemaTypes(t)
	}
	for _, ty := range types {
		if ty == "object" || ty == "array" {
			return true
		}
	}
	_, hasProps := node.raw("properties")
	_, hasItems := node.raw("items")
	return hasProps || hasItems
}

func schemaAllowsNull(data json.RawMessage) bool {
	node, err := decodeSchemaNode(data)
	if err != nil {
		return false
	}
	if t, ok := node.raw("type"); ok {
		for _, ty := range schemaTypes(t) {
			if ty == "null" {
				return true
			}
		}
	}
	if c, ok := node.raw("const"); ok && string(c) == "null" {
		return true
	}
	if e, ok := node.raw("enum"); ok {
		var list []json.RawMessage
		if err := jsonUnmarshalStrict(e, &list); err == nil {
			for _, v := range list {
				if string(v) == "null" {
					return true
				}
			}
		}
	}
	if anyOf, ok := node.raw("anyOf"); ok {
		var list []json.RawMessage
		if err := jsonUnmarshalStrict(anyOf, &list); err == nil {
			for _, variant := range list {
				if schemaAllowsNull(variant) {
					return true
				}
			}
		}
	}
	return false
}

// UnsupportedStrictSchemaKeywordCheck reports whether a provider's strict mode
// rejects a schema keyword with this value (upstream
// UnsupportedStrictSchemaKeywordCheck, 295cc72b0).
type UnsupportedStrictSchemaKeywordCheck func(key string, value json.RawMessage) bool

// AnthropicStrictUnsupportedKeyword is Anthropic's check: strict tool use
// rejects these keywords with a 400 for the whole request, so "prefer" tools
// that hit one fall back to non-strict.
// https://platform.claude.com/docs/en/build-with-claude/structured-outputs#json-schema-limitations
func AnthropicStrictUnsupportedKeyword(key string, value json.RawMessage) bool {
	switch key {
	case "minimum", "maximum", "exclusiveMinimum", "exclusiveMaximum", "multipleOf",
		"maxItems", "uniqueItems", "minContains", "maxContains", "minProperties", "maxProperties":
		return true
	case "minItems":
		number := decodeSchemaValue(value)
		return !number.IsNumber || (number.Number != 0 && number.Number != 1)
	case "format":
		var format string
		if jsonUnmarshalStrict(value, &format) != nil {
			return true
		}
		return !anthropicStrictStringFormats[format]
	}
	return false
}

var anthropicStrictStringFormats = map[string]bool{
	"date-time": true, "time": true, "date": true, "duration": true,
	"email": true, "hostname": true, "uri": true, "ipv4": true, "ipv6": true, "uuid": true,
}

func makeSchemaNodeStrict(node *schemaNode, check UnsupportedStrictSchemaKeywordCheck) error {
	for _, key := range unsupportedStrictSchemaKeys {
		if _, ok := node.raw(key); ok {
			return &strictSchemaError{msg: key + " schemas are unsupported"}
		}
	}
	if check != nil {
		for _, key := range node.keys {
			if check(key, node.fields[key]) {
				return &strictSchemaError{msg: key + " is unsupported"}
			}
		}
	}

	if anyOfData, ok := node.raw("anyOf"); ok {
		var list []json.RawMessage
		if err := jsonUnmarshalStrict(anyOfData, &list); err != nil || len(list) == 0 {
			return &strictSchemaError{msg: "anyOf must contain at least one schema"}
		}
		for i, variant := range list {
			if isStructuredSchema(variant) {
				return &strictSchemaError{msg: "object and array unions are unsupported"}
			}
			strict, err := makeStrictSubSchemaValue(variant, check)
			if err != nil {
				return err
			}
			list[i] = strict
		}
		enc, _ := MarshalJSON(list)
		node.set("anyOf", enc)
	}

	if itemsData, ok := node.raw("items"); ok {
		var list []json.RawMessage
		if err := jsonUnmarshalStrict(itemsData, &list); err == nil {
			return &strictSchemaError{msg: "tuple schemas are unsupported"}
		}
		strict, err := makeStrictSubSchemaValue(itemsData, check)
		if err != nil {
			return err
		}
		node.set("items", strict)
	}

	var objectType string
	if t, ok := node.raw("type"); ok {
		_ = jsonUnmarshalStrict(t, &objectType)
	}
	isObjectSchema := objectType == "object"

	if _, hasProps := node.raw("properties"); hasProps && !isObjectSchema {
		return &strictSchemaError{msg: "properties require type object"}
	}
	if !isObjectSchema {
		return nil
	}
	if ap, ok := node.raw("additionalProperties"); ok && string(ap) != "false" {
		return &strictSchemaError{msg: "schema-valued or true additionalProperties is unsupported"}
	}
	propsData, hasProps := node.raw("properties")
	var properties map[string]json.RawMessage
	if hasProps {
		if err := jsonUnmarshalStrict(propsData, &properties); err != nil || properties == nil {
			return &strictSchemaError{msg: "object properties must be a schema map"}
		}
	}
	var required []string
	if reqData, ok := node.raw("required"); ok {
		if err := jsonUnmarshalStrict(reqData, &required); err != nil || required == nil {
			return &strictSchemaError{msg: "object required must be a string array"}
		}
	}

	// Object.keys uses numeric-index order before other insertion-ordered keys.
	orderedProps := orderedKeys(propsData)

	requiredSet := map[string]bool{}
	for _, key := range required {
		requiredSet[key] = true
	}
	for _, key := range required {
		if _, ok := properties[key]; !ok {
			return &strictSchemaError{msg: "required contains an unknown property"}
		}
	}
	for _, key := range orderedProps {
		property, err := makeStrictSubSchemaValue(properties[key], check)
		if err != nil {
			return err
		}
		if !requiredSet[key] && !schemaAllowsNull(property) {
			property, _ = MarshalJSON(struct {
				AnyOf []json.RawMessage `json:"anyOf"`
			}{[]json.RawMessage{property, json.RawMessage(`{"type":"null"}`)}})
		}
		properties[key] = property
	}
	newRequired := append([]string{}, orderedProps...)
	reqEnc, _ := MarshalJSON(newRequired)
	node.set("required", reqEnc)
	node.set("additionalProperties", json.RawMessage("false"))
	if hasProps {
		propEnc, err := (&schemaNode{fields: properties, keys: orderedProps}).encode()
		if err != nil {
			return err
		}
		node.set("properties", propEnc)
	}
	return nil
}

// orderedKeys recovers JavaScript Object.keys order from a JSON object.
func orderedKeys(data json.RawMessage) []string {
	dec := json.NewDecoder(strings.NewReader(string(data)))
	tok, err := dec.Token()
	if err != nil {
		return nil
	}
	if delim, ok := tok.(json.Delim); !ok || delim != '{' {
		return nil
	}
	var keys []string
	seen := make(map[string]bool)
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return keys
		}
		key := tok.(string)
		if !seen[key] {
			keys = appendJSObjectKey(keys, key)
			seen[key] = true
		}
		var raw json.RawMessage
		_ = dec.Decode(&raw)
	}
	return keys
}

// makeStrictSubSchemaValue returns the strict encoding of a sub-schema without
// the root object requirement: upstream recurses anyOf variants and array items
// through makeJsonSchemaNodeStrict directly, and only the tool's root schema
// must be an object.
func makeStrictSubSchemaValue(data json.RawMessage, check UnsupportedStrictSchemaKeywordCheck) (json.RawMessage, error) {
	node, err := decodeSchemaNode(data)
	if err != nil {
		return nil, &strictSchemaError{msg: "boolean schemas are unsupported"}
	}
	if err := makeSchemaNodeStrict(node, check); err != nil {
		return nil, err
	}
	enc, err := node.encode()
	if err != nil {
		return nil, err
	}
	return enc, nil
}

// makeStrictSubSchema returns the strict encoding of a sub-schema.
func makeStrictSubSchema(data json.RawMessage, check UnsupportedStrictSchemaKeywordCheck) (json.RawMessage, error) {
	strict, err := makeStrictSubSchemaValue(data, check)
	if err != nil {
		return nil, err
	}
	node, err := decodeSchemaNode(strict)
	if err != nil {
		return nil, &strictSchemaError{msg: "root schema must have type object"}
	}
	t, _ := node.raw("type")
	var objectType string
	if jsonUnmarshalStrict(t, &objectType) != nil || objectType != "object" {
		return nil, &strictSchemaError{msg: "root schema must have type object"}
	}
	return strict, nil
}

// MakeStrictJSONSchema converts a tool schema to the strict subset expected
// by provider constrained sampling (port of makeStrictJsonSchema).
func MakeStrictJSONSchema(schema json.RawMessage) (json.RawMessage, error) {
	return makeStrictSubSchema(schema, nil)
}

// GetJSONSchemaToolParameters returns the tool parameters for a strictness
// mode (port of getJsonSchemaToolParameters).
func GetJSONSchemaToolParameters(tool Tool, strict bool) json.RawMessage {
	if !strict {
		return tool.Parameters
	}
	strictSchema, err := MakeStrictJSONSchema(tool.Parameters)
	if err != nil {
		return tool.Parameters
	}
	return strictSchema
}

// ResolveJSONSchemaStrictSampling resolves whether a tool's json_schema
// constrained sampling applies (port of resolveJsonSchemaStrictSampling).
func ResolveJSONSchemaStrictSampling(tool Tool, supportsStrictMode bool, check UnsupportedStrictSchemaKeywordCheck) (strict bool, unset bool, err error) {
	config := tool.ConstrainedSampling
	if !config.Set || config.False || config.Config == nil || config.Config.Type != "json_schema" {
		return false, true, nil
	}
	cfg := config.Config
	if supportsStrictMode {
		if _, err := makeStrictSubSchema(tool.Parameters, check); err != nil {
			if _, ok := err.(*strictSchemaError); ok {
				if cfg.Strict != "require" {
					return false, true, nil
				}
				return false, false, fmt.Errorf("Tool %q requires JSON-schema constrained sampling, but %s.", tool.Name, err.Error())
			}
			return false, false, err
		}
		return true, false, nil
	}
	if cfg.Strict == "require" {
		return false, false, fmt.Errorf("Tool %q requires JSON-schema constrained sampling, but strict tools are unsupported.", tool.Name)
	}
	return false, true, nil
}
