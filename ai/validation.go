package ai

import (
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
)

// Tool-argument validation. The pi-schema port lives in validation_pi*.go;
// this file keeps the ordered JSON value, literal equality and the public
// ValidateToolArguments entry point.

type schemaValue struct {
	Null     bool
	Bool     bool
	Number   float64
	String   string
	IsBool   bool
	IsNumber bool
	IsString bool
	IsArray  bool
	Array    []*schemaValue
	Object   *schemaObject
	// BigInt is a bigint schema's coerced value, kept as decimal digits. The
	// probes record it the way their JSON replacer renders a JS BigInt
	// ("1n"), and pi v1.5.2 b4afdddf packages/schema/src/coerce.ts is the rule
	// that produces it.
	IsBigInt bool
	BigInt   string
	// Undefined is a value a schema converted to undefined: JSON has no such
	// value, so an object property holding it is dropped and an array slot
	// becomes null, as JSON.stringify does.
	IsUndefined bool
}

type schemaObject struct {
	keys   []string
	values map[string]*schemaValue
}

func (o *schemaObject) get(key string) (*schemaValue, bool) {
	if o == nil {
		return nil, false
	}
	v, ok := o.values[key]
	return v, ok
}

func (o *schemaObject) set(key string, value *schemaValue) {
	if _, ok := o.values[key]; !ok {
		o.keys = appendJSObjectKey(o.keys, key)
	}
	o.values[key] = value
}

func (o *schemaObject) delete(key string) {
	delete(o.values, key)
	for i, k := range o.keys {
		if k == key {
			o.keys = append(o.keys[:i], o.keys[i+1:]...)
			break
		}
	}
}

// decodeSchemaValue decodes JSON preserving object key order.
func decodeSchemaValue(data []byte) *schemaValue {
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.UseNumber()
	tok, err := dec.Token()
	if err != nil {
		return &schemaValue{}
	}
	return decodeTokenValue(dec, tok)
}

func decodeTokenValue(dec *json.Decoder, tok json.Token) *schemaValue {
	switch t := tok.(type) {
	case nil:
		return &schemaValue{Null: true}
	case bool:
		return &schemaValue{Bool: t, IsBool: true}
	case json.Number:
		f, _ := t.Float64()
		return &schemaValue{Number: f, IsNumber: true}
	case string:
		return &schemaValue{String: t, IsString: true}
	case json.Delim:
		switch t {
		case '{':
			out := &schemaValue{Object: &schemaObject{values: map[string]*schemaValue{}}}
			for dec.More() {
				keyTok, _ := dec.Token()
				key := keyTok.(string)
				var raw json.RawMessage
				_ = dec.Decode(&raw)
				inner := decodeSchemaValue(raw)
				out.Object.set(key, inner)
			}
			_, _ = dec.Token() // }
			return out
		case '[':
			out := &schemaValue{IsArray: true}
			for dec.More() {
				var raw json.RawMessage
				_ = dec.Decode(&raw)
				out.Array = append(out.Array, decodeSchemaValue(raw))
			}
			_, _ = dec.Token() // ]
			return out
		}
	}
	return &schemaValue{}
}

// encodeSchemaValue re-encodes preserving key order.
func encodeSchemaValue(v *schemaValue) json.RawMessage {
	switch {
	case v.Null:
		return json.RawMessage("null")
	case v.IsBool:
		return mustMarshalJSON(v.Bool)
	case v.IsNumber:
		if math.IsNaN(v.Number) || math.IsInf(v.Number, 0) {
			return json.RawMessage("null")
		}
		return json.RawMessage(JSNumberToString(v.Number))
	case v.IsBigInt:
		return mustMarshalJSON(v.BigInt + "n")
	case v.IsUndefined:
		// JSON.stringify renders an undefined array slot as null.
		return json.RawMessage("null")
	case v.IsString:
		return mustMarshalJSON(v.String)
	case v.IsArray:
		out := []byte{'['}
		for i, item := range v.Array {
			if i > 0 {
				out = append(out, ',')
			}
			out = append(out, encodeSchemaValue(item)...)
		}
		return append(out, ']')
	case v.Object != nil:
		out := []byte{'{'}
		written := 0
		for _, key := range v.Object.keys {
			value := v.Object.values[key]
			if value.IsUndefined {
				continue // JSON.stringify drops an undefined property
			}
			if written > 0 {
				out = append(out, ',')
			}
			written++
			keyEnc := mustMarshalJSON(key)
			out = append(out, keyEnc...)
			out = append(out, ':')
			out = append(out, encodeSchemaValue(value)...)
		}
		return append(out, '}')
	default:
		return json.RawMessage("null")
	}
}

func matchesJSONType(value *schemaValue, typ string) bool {
	switch typ {
	case "number":
		// pi v1.5.2 b4afdddf matchesType: Number.isFinite(value), so a
		// non-finite argument is not a number.
		return value.IsNumber && isFiniteJSONNumber(value.Number)
	case "bigint":
		return value.IsBigInt
	case "undefined":
		return value.IsUndefined
	case "integer":
		return value.IsNumber && !math.IsInf(value.Number, 0) && value.Number == math.Trunc(value.Number)
	case "boolean":
		return value.IsBool
	case "string":
		return value.IsString
	case "null":
		return value.Null
	case "array":
		return value.IsArray
	case "object":
		return value.Object != nil
	default:
		return false
	}
}

var jsDecimalNumberPattern = regexp.MustCompile(`^[+-]?(?:(?:[0-9]+(?:\.[0-9]*)?|\.[0-9]+)(?:[eE][+-]?[0-9]+)?|Infinity)$`)

// ParseJSNumberString parses a nonempty, finite JavaScript numeric string,
// including hexadecimal, binary and octal literals.
func ParseJSNumberString(value string) (float64, bool) {
	value = JSTrim(value)
	if value == "" {
		return 0, false
	}
	if len(value) > 2 && value[0] == '0' {
		base := 0
		switch value[1] {
		case 'x', 'X':
			base = 16
		case 'b', 'B':
			base = 2
		case 'o', 'O':
			base = 8
		}
		if base != 0 {
			if value[2] == '+' || value[2] == '-' {
				return 0, false
			}
			number, ok := new(big.Int).SetString(value[2:], base)
			if !ok {
				return 0, false
			}
			parsed, _ := number.Float64()
			return parsed, !math.IsInf(parsed, 0)
		}
	}
	if !jsDecimalNumberPattern.MatchString(value) {
		return 0, false
	}
	parsed, err := strconv.ParseFloat(value, 64)
	return parsed, err == nil && !math.IsNaN(parsed) && !math.IsInf(parsed, 0)
}

// coercePrimitiveByType is pass 3 of the reference coercion (coerce.ts
// coercePrimitive), which runs only for a type the value does not already
// match. Pass 2 (convertNode) is the TypeBox conversion and is stricter
// elsewhere: it truncates an integer and reads boolean words as numbers, while
// this pass leaves a number alone for "integer" and turns null into "" rather
// than "null".
func coercePrimitiveByType(value *schemaValue, typ string) *schemaValue {
	switch typ {
	case "number", "integer":
		if value.Null {
			return &schemaValue{IsNumber: true, Number: 0}
		}
		if value.IsString && JSTrim(value.String) != "" {
			if parsed, ok := ParseJSNumberString(value.String); ok && (typ == "number" || isIntegerFloat(parsed)) {
				return &schemaValue{IsNumber: true, Number: parsed}
			}
		}
		if value.IsBool {
			n := 0.0
			if value.Bool {
				n = 1
			}
			return &schemaValue{IsNumber: true, Number: n}
		}
		return value
	case "boolean":
		if value.Null {
			return &schemaValue{IsBool: true, Bool: false}
		}
		if value.IsString {
			switch value.String {
			case "true":
				return &schemaValue{IsBool: true, Bool: true}
			case "false":
				return &schemaValue{IsBool: true, Bool: false}
			}
		}
		if value.IsNumber {
			if value.Number == 1 {
				return &schemaValue{IsBool: true, Bool: true}
			}
			if value.Number == 0 {
				return &schemaValue{IsBool: true, Bool: false}
			}
		}
		return value
	case "string":
		if value.Null {
			// Pass 3 reads null as ""; pass 2's tryString reads it as "null".
			return &schemaValue{IsString: true, String: ""}
		}
		if value.IsNumber {
			return &schemaValue{IsString: true, String: JSNumberToString(value.Number)}
		}
		if value.IsBool {
			if value.Bool {
				return &schemaValue{IsString: true, String: "true"}
			}
			return &schemaValue{IsString: true, String: "false"}
		}
		return value
	case "null":
		if (value.IsString && value.String == "") || (value.IsNumber && value.Number == 0) || (value.IsBool && !value.Bool) {
			return &schemaValue{Null: true}
		}
		return value
	default:
		return value
	}
}

// isIntegerFloat is Number.isInteger: finite and without a fractional part.
func isIntegerFloat(f float64) bool {
	return !math.IsInf(f, 0) && !math.IsNaN(f) && math.Trunc(f) == f
}

func schemaValueEqual(a, b *schemaValue) bool {
	if a == b {
		return true
	}
	return string(encodeSchemaValue(a)) == string(encodeSchemaValue(b))
}

// schemaLiteralIn reports whether value equals any of options under
// schemaLiteralEqual, the enum and discriminator-tag membership test.
func schemaLiteralIn(value *schemaValue, options []*schemaValue) bool {
	return slices.ContainsFunc(options, func(option *schemaValue) bool { return schemaLiteralEqual(value, option) })
}

// TypeBox compares const/enum values structurally, not their JSON spelling.
// Object member order does not matter; array order and primitive types do.
func schemaLiteralEqual(a, b *schemaValue) bool {
	if a.Null || b.Null {
		return a.Null && b.Null
	}
	if a.IsBool || b.IsBool {
		return a.IsBool && b.IsBool && a.Bool == b.Bool
	}
	if a.IsNumber || b.IsNumber {
		return a.IsNumber && b.IsNumber && a.Number == b.Number
	}
	if a.IsString || b.IsString {
		return a.IsString && b.IsString && a.String == b.String
	}
	if a.IsArray || b.IsArray {
		if !a.IsArray || !b.IsArray || len(a.Array) != len(b.Array) {
			return false
		}
		for i, value := range a.Array {
			if !schemaLiteralEqual(value, b.Array[i]) {
				return false
			}
		}
		return true
	}
	if a.Object == nil || b.Object == nil || len(a.Object.keys) != len(b.Object.keys) {
		return false
	}
	for _, key := range a.Object.keys {
		other, ok := b.Object.get(key)
		if !ok || !schemaLiteralEqual(a.Object.values[key], other) {
			return false
		}
	}
	return true
}

var piValidatorCache sync.Map // string(schema) -> *piDoc

func getPiValidator(schema json.RawMessage) (*piDoc, *piSchema, error) {
	key := string(schema)
	if cached, ok := piValidatorCache.Load(key); ok {
		doc := cached.(*piDoc)
		return doc, doc.rootSchema, doc.err
	}
	doc, root, err := parsePiSchema(schema)
	piValidatorCache.Store(key, doc)
	return doc, root, err
}

// ValidateToolArguments validates (and coerces) tool call arguments against
// the tool's schema (port of validateToolArguments). Returns the validated
// arguments or an error with the formatted validation message.
func ValidateToolArguments(tool Tool, toolCall ToolCall) (json.RawMessage, error) {
	// A finished call whose complete arguments text was not a JSON object fails
	// with the text the model sent, rather than validating the empty object
	// (#10944).
	if toolCall.ArgumentsError != "" {
		return nil, fmt.Errorf("Invalid arguments for tool \"%s\": %s", toolCall.Name, toolCall.ArgumentsError)
	}
	_, schema, schemaErr := getPiValidator(tool.Parameters)
	if schemaErr != nil {
		return nil, schemaErr
	}
	args := decodeSchemaValue(toolCall.Arguments)
	args = schema.coerce(args)

	if schema.check(args) {
		return encodeSchemaValue(args), nil
	}

	errors := schema.errors(args)
	errorLines := make([]string, 0, len(errors))
	for _, validationError := range errors {
		errorLines = append(errorLines, "  - "+formatValidationPath(validationError.path)+": "+validationError.message)
	}
	var received = toolCall.Arguments
	if len(toolCall.Arguments) > 0 {
		if formatted, err := MarshalIndentJSON(toolCall.Arguments); err == nil {
			received = formatted
		}
	}
	return nil, fmt.Errorf("Validation failed for tool \"%s\":\n%s\n\nReceived arguments:\n%s",
		toolCall.Name, strings.Join(errorLines, "\n"), received)
}
