package ai

import (
	"bytes"
	"encoding/json"
	"math"
	"strconv"
	"strings"
)

// schemaErrorLines follows TypeBox's error walk for the supported validation
// subset. A container passing its own type check must never get a type error
// merely because a nested property failed.
func schemaErrorLines(value *schemaValue, schema *jsonSchema, path string) []string {
	if schema == nil {
		return nil
	}
	var lines []string
	add := func(at, message string) {
		if at == "" {
			at = "root"
		}
		lines = append(lines, "  - "+at+": "+message)
	}
	childPath := func(key string) string {
		key = strings.ReplaceAll(key, "/", ".")
		if path == "" {
			return key
		}
		return path + "." + key
	}
	if schema.RejectAll {
		add(path, "schema is false")
		return lines
	}
	if len(schema.Types) > 0 {
		matched := false
		for _, typ := range schema.Types {
			if matchesJSONType(value, typ) && !(typ == "number" && (math.IsInf(value.Number, 0) || math.IsNaN(value.Number))) {
				matched = true
			}
		}
		if !matched {
			message := "must be " + schema.Types[0]
			if bytes.HasPrefix(bytes.TrimSpace(schema.Raw["type"]), []byte("[")) {
				message = "must be either " + strings.Join(schema.Types, " or ")
			}
			add(path, message)
		}
	}
	if value.Object != nil {
		var required, missing []string
		_ = json.Unmarshal(schema.Raw["required"], &required)
		for _, key := range required {
			if _, ok := value.Object.get(key); !ok {
				missing = append(missing, key)
			}
		}
		if len(missing) > 0 {
			add(childPath(missing[0]), "must have required properties "+strings.Join(missing, ", "))
		}
		if schema.HasAdditionalProperties {
			invalid := false
			for _, key := range value.Object.keys {
				if _, declared := schema.Properties[key]; declared {
					continue
				}
				field, _ := value.Object.get(key)
				if schema.AdditionalProperties == nil {
					add(childPath(key), "schema is false")
					invalid = true
				} else {
					nested := schemaErrorLines(field, schema.AdditionalProperties, childPath(key))
					invalid = invalid || len(nested) > 0
					lines = append(lines, nested...)
				}
			}
			if invalid {
				add(path, "must not have additional properties")
			}
		}
		for _, key := range schema.PropertyOrder {
			if field, ok := value.Object.get(key); ok {
				lines = append(lines, schemaErrorLines(field, schema.Properties[key], childPath(key))...)
			}
		}
	}
	if value.IsArray {
		for i, item := range value.Array {
			nested := schema.ItemsOne
			if i < len(schema.Items) {
				nested = schema.Items[i]
			}
			if nested != nil {
				lines = append(lines, schemaErrorLines(item, nested, childPath(strconv.Itoa(i)))...)
			}
		}
	}
	if schema.Const != nil && !schemaLiteralEqual(value, decodeSchemaValue(schema.Const)) {
		add(path, "must be equal to constant")
	}
	if schema.Enum != nil {
		matched := false
		for _, option := range schema.Enum {
			if schemaLiteralEqual(value, decodeSchemaValue(option)) {
				matched = true
				break
			}
		}
		if !matched {
			add(path, "must be equal to one of the allowed values")
		}
	}
	for _, nested := range schema.AllOf {
		lines = append(lines, schemaErrorLines(value, nested, path)...)
	}
	for _, union := range []struct {
		schemas []*jsonSchema
		one     bool
	}{{schema.AnyOf, false}, {schema.OneOf, true}} {
		if union.schemas == nil {
			continue
		}
		matches := 0
		var failures []string
		for _, nested := range union.schemas {
			errors := schemaErrorLines(value, nested, path)
			if len(errors) == 0 {
				matches++
			} else {
				failures = append(failures, errors...)
			}
		}
		if matches == 0 || union.one && matches != 1 {
			if matches == 0 {
				lines = append(lines, failures...)
			}
			message := "must match a schema in anyOf"
			if union.one {
				message = "must match exactly one schema in oneOf"
			}
			add(path, message)
		}
	}
	return lines
}
