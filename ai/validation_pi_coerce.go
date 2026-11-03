package ai

import (
	"math"
	"strconv"
	"strings"
)

// Port of pi v1.5.0 packages/schema/src/coerce.ts, including TypeBox 1.3.27's
// conversion rules. Every pass can mutate the decoded tool arguments: they are
// already a private copy of the tool call.

type piCoerceVisit struct {
	schema *piSchema
	value  *schemaValue
}

func (s *piSchema) coerce(value *schemaValue) *schemaValue {
	value = s.dropOptionalNulls(value, map[piCoerceVisit]bool{})
	value = s.convert(value, map[piCoerceVisit]bool{})
	return s.coerceTypes(value, map[piCoerceVisit]bool{})
}

func (s *piSchema) dropOptionalNulls(value *schemaValue, active map[piCoerceVisit]bool) *schemaValue {
	if s == nil || value == nil || s.false {
		return value
	}
	visit := piCoerceVisit{s, value}
	if active[visit] {
		return value
	}
	active[visit] = true
	defer delete(active, visit)
	if value.IsArray {
		for i, item := range value.Array {
			if schema := s.itemAt(i); schema != nil {
				value.Array[i] = schema.dropOptionalNulls(item, active)
			}
		}
		return value
	}
	if value.Object == nil || !s.hasObjectKeywords() {
		return value
	}
	for _, key := range append([]string(nil), value.Object.keys...) {
		field, ok := value.Object.get(key)
		if !ok {
			continue
		}
		property, declared := s.properties[key]
		if declared {
			if field.Null && !s.requiredSet[key] && property.ref == "" && !property.check(field) {
				value.Object.delete(key)
				continue
			}
			value.Object.set(key, property.dropOptionalNulls(field, active))
		}
		for _, pattern := range s.patternOrder {
			if re := s.patternRes[pattern]; re != nil && re.MatchString(key) {
				value.Object.set(key, s.patternProps[pattern].dropOptionalNulls(field, active))
			}
		}
		if !declared && s.additionalProps != nil {
			value.Object.set(key, s.additionalProps.dropOptionalNulls(field, active))
		}
	}
	return value
}

func (s *piSchema) convert(value *schemaValue, active map[piCoerceVisit]bool) *schemaValue {
	if s == nil || value == nil || s.false {
		return value
	}
	visit := piCoerceVisit{s, value}
	// Union candidates are private clones. A clone of an already-active value
	// must not evade the recursion guard when a scalar is wrapped again.
	for entered := range active {
		if entered.schema == s && schemaLiteralEqual(entered.value, value) {
			return value
		}
	}
	active[visit] = true
	defer delete(active, visit)

	if s.ref != "" {
		if target, ok := s.resolve(); ok && target != nil {
			return target.convert(value, active)
		}
		return value
	}
	if s.hasConst {
		return convertPiLiteral(s.constVal, value)
	}
	if s.hasEnum {
		for _, member := range s.enum {
			if schemaLiteralEqual(member, value) {
				return value
			}
		}
		for _, member := range s.enum {
			candidate := convertPiLiteral(member, cloneSchemaValue(value))
			if candidate != value && s.check(candidate) {
				return candidate
			}
		}
		return value
	}
	if s.anyOf != nil {
		return s.convertUnion(s.anyOf, value, active)
	}
	if s.oneOf != nil {
		return s.convertUnion(s.oneOf, value, active)
	}
	if s.allOf != nil {
		for _, member := range s.allOf {
			value = member.convert(value, active)
		}
		return s.convertMembers(value, active)
	}
	if len(s.types) == 1 {
		switch s.types[0] {
		case "string":
			if converted, ok := tryPiString(value); ok {
				return converted
			}
			return value
		case "number":
			if converted, ok := tryPiNumber(value); ok {
				return converted
			}
			return value
		case "integer":
			if converted, ok := tryPiNumber(value); ok {
				return &schemaValue{IsNumber: true, Number: math.Trunc(converted.Number)}
			}
			return value
		case "boolean":
			if converted, ok := tryPiBoolean(value); ok {
				return converted
			}
			return value
		case "null":
			if converted, ok := tryPiNull(value); ok {
				return converted
			}
			return value
		case "array":
			return s.convertArray(value, active)
		case "object":
			return s.convertObject(value, active)
		}
	}
	if len(s.types) == 0 {
		if s.properties != nil || s.patternProps != nil || s.additionalProps != nil {
			return s.convertObject(value, active)
		}
		if s.itemsOne != nil || s.items != nil || s.prefixItems != nil {
			return s.convertArray(value, active)
		}
	}
	return value
}

func (s *piSchema) convertUnion(branches []*piSchema, value *schemaValue, active map[piCoerceVisit]bool) *schemaValue {
	for _, branch := range branches {
		if branch.check(value) {
			return s.convertMembers(value, active)
		}
	}
	for _, branch := range branches {
		candidate := branch.convert(cloneSchemaValue(value), active)
		candidate = s.convertMembers(candidate, active)
		if s.check(candidate) {
			return candidate
		}
	}
	return s.convertMembers(value, active)
}

func (s *piSchema) convertMembers(value *schemaValue, active map[piCoerceVisit]bool) *schemaValue {
	if s.properties != nil || s.patternProps != nil || s.additionalProps != nil {
		return s.convertObject(value, active)
	}
	return value
}

func (s *piSchema) convertArray(value *schemaValue, active map[piCoerceVisit]bool) *schemaValue {
	prefix := s.items
	if s.prefixItems != nil {
		prefix = s.prefixItems
	}
	if prefix != nil {
		if !value.IsArray {
			return value
		}
		rest := s.itemsOne
		if s.items != nil {
			rest = nil
		}
		for i, item := range value.Array {
			var schema *piSchema
			if i < len(prefix) {
				schema = prefix[i]
			} else {
				schema = rest
			}
			if schema != nil {
				value.Array[i] = schema.convert(item, active)
			}
		}
		return value
	}
	if value.IsArray {
		if s.itemsOne != nil {
			for i, item := range value.Array {
				value.Array[i] = s.itemsOne.convert(item, active)
			}
		}
		return value
	}
	item := value
	if s.itemsOne != nil {
		item = s.itemsOne.convert(item, active)
	}
	return &schemaValue{IsArray: true, Array: []*schemaValue{item}}
}

func (s *piSchema) convertObject(value *schemaValue, active map[piCoerceVisit]bool) *schemaValue {
	if value.Object == nil {
		return value
	}
	for _, key := range append([]string(nil), value.Object.keys...) {
		field, _ := value.Object.get(key)
		next := field
		matched := false
		if property, ok := s.properties[key]; ok {
			matched = true
			next = property.convert(next, active)
		}
		for _, pattern := range s.patternOrder {
			if re := s.patternRes[pattern]; re != nil && re.MatchString(key) {
				matched = true
				next = s.patternProps[pattern].convert(next, active)
			}
		}
		if !matched && s.additionalProps != nil {
			next = s.additionalProps.convert(next, active)
		}
		if next != field {
			value.Object.set(key, next)
		}
	}
	return value
}

func (s *piSchema) coerceTypes(value *schemaValue, active map[piCoerceVisit]bool) *schemaValue {
	if s == nil || value == nil || s.false {
		return value
	}
	visit := piCoerceVisit{s, value}
	if active[visit] {
		return value
	}
	active[visit] = true
	defer delete(active, visit)
	next := value
	for _, member := range s.allOf {
		next = member.coerceTypes(next, active)
	}
	if s.anyOf != nil {
		next = s.coerceUnion(s.anyOf, next, active)
	}
	if s.oneOf != nil {
		next = s.coerceUnion(s.oneOf, next, active)
	}
	types := s.types
	matchesMember := false
	if len(types) > 1 {
		for _, typ := range types {
			if matchesJSONType(next, typ) {
				matchesMember = true
			}
		}
	}
	if len(types) > 0 && !matchesMember {
		for _, typ := range types {
			candidate := coercePrimitiveByType(next, typ)
			if !schemaValueEqual(candidate, next) {
				next = candidate
				break
			}
		}
	}
	if containsType(types, "object") && next.Object != nil {
		for _, key := range append([]string(nil), next.Object.keys...) {
			field, _ := next.Object.get(key)
			coerced := field
			if property, ok := s.properties[key]; ok {
				coerced = property.coerceTypes(coerced, active)
			}
			for _, pattern := range s.patternOrder {
				if re := s.patternRes[pattern]; re != nil && re.MatchString(key) {
					coerced = s.patternProps[pattern].coerceTypes(coerced, active)
				}
			}
			if _, declared := s.properties[key]; !declared && !patternMatches(s, key) && s.additionalProps != nil {
				coerced = s.additionalProps.coerceTypes(coerced, active)
			}
			if coerced != field {
				next.Object.set(key, coerced)
			}
		}
	}
	if containsType(types, "array") && next.IsArray {
		for i, item := range next.Array {
			if schema := s.itemAt(i); schema != nil {
				next.Array[i] = schema.coerceTypes(item, active)
			}
		}
	}
	return next
}

func (s *piSchema) coerceUnion(branches []*piSchema, value *schemaValue, active map[piCoerceVisit]bool) *schemaValue {
	for _, branch := range branches {
		if branch.check(value) {
			return value
		}
	}
	for _, branch := range branches {
		candidate := branch.coerceTypes(cloneSchemaValue(value), active)
		if branch.check(candidate) {
			return candidate
		}
	}
	return value
}

func patternMatches(s *piSchema, key string) bool {
	for _, pattern := range s.patternOrder {
		if re := s.patternRes[pattern]; re != nil && re.MatchString(key) {
			return true
		}
	}
	return false
}

func containsType(types []string, want string) bool {
	for _, typ := range types {
		if typ == want {
			return true
		}
	}
	return false
}

func cloneSchemaValue(value *schemaValue) *schemaValue {
	return decodeSchemaValue(encodeSchemaValue(value))
}

func convertPiLiteral(literal, value *schemaValue) *schemaValue {
	if schemaLiteralEqual(literal, value) {
		return value
	}
	var converted *schemaValue
	var ok bool
	switch {
	case literal == nil:
		return value
	case literal.IsString:
		converted, ok = tryPiString(value)
	case literal.IsNumber:
		converted, ok = tryPiNumber(value)
	case literal.IsBool:
		converted, ok = tryPiBoolean(value)
	default:
		return value
	}
	if ok && schemaLiteralEqual(converted, literal) {
		return converted
	}
	return value
}

func tryPiString(value *schemaValue) (*schemaValue, bool) {
	switch {
	case value.IsString:
		return value, true
	case value.Null:
		return &schemaValue{IsString: true, String: "null"}, true
	case value.IsBool:
		if value.Bool {
			return &schemaValue{IsString: true, String: "true"}, true
		}
		return &schemaValue{IsString: true, String: "false"}, true
	case value.IsNumber:
		if isFiniteJSONNumber(value.Number) {
			return &schemaValue{IsString: true, String: JSNumberToString(value.Number)}, true
		}
	}
	return nil, false
}

func tryPiNumber(value *schemaValue) (*schemaValue, bool) {
	switch {
	case value.IsNumber:
		return value, true
	case value.Null:
		return &schemaValue{IsNumber: true, Number: 0}, true
	case value.IsBool:
		if value.Bool {
			return &schemaValue{IsNumber: true, Number: 1}, true
		}
		return &schemaValue{IsNumber: true, Number: 0}, true
	case value.IsString:
		text := JSTrim(value.String)
		if text == "" {
			return &schemaValue{IsNumber: true, Number: 0}, true
		}
		if parsed, ok := ParseJSNumberString(text); ok {
			return &schemaValue{IsNumber: true, Number: parsed}, true
		}
		if lower := strings.ToLower(text); lower == "true" {
			return &schemaValue{IsNumber: true, Number: 1}, true
		} else if lower == "false" {
			return &schemaValue{IsNumber: true, Number: 0}, true
		}
		if strings.HasSuffix(text, "n") {
			if n, err := strconv.ParseInt(strings.TrimSuffix(text, "n"), 10, 64); err == nil {
				return &schemaValue{IsNumber: true, Number: float64(n)}, true
			}
		}
	}
	return nil, false
}

func tryPiBoolean(value *schemaValue) (*schemaValue, bool) {
	switch {
	case value.IsBool:
		return value, true
	case value.Null:
		return &schemaValue{IsBool: true, Bool: false}, true
	case value.IsNumber:
		if value.Number == 0 {
			return &schemaValue{IsBool: true, Bool: false}, true
		}
		if value.Number == 1 {
			return &schemaValue{IsBool: true, Bool: true}, true
		}
	case value.IsString:
		lower := strings.ToLower(value.String)
		if lower == "false" || value.String == "0" {
			return &schemaValue{IsBool: true, Bool: false}, true
		}
		if lower == "true" || value.String == "1" {
			return &schemaValue{IsBool: true, Bool: true}, true
		}
	}
	return nil, false
}

func tryPiNull(value *schemaValue) (*schemaValue, bool) {
	switch {
	case value.Null:
		return value, true
	case value.IsBool:
		if !value.Bool {
			return &schemaValue{Null: true}, true
		}
	case value.IsNumber:
		if value.Number == 0 {
			return &schemaValue{Null: true}, true
		}
	case value.IsString:
		lower := strings.ToLower(value.String)
		if lower == "undefined" || lower == "null" || value.String == "" || value.String == "0" {
			return &schemaValue{Null: true}, true
		}
	}
	return nil, false
}
