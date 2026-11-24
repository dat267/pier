package ai

import (
	"math"
	"strconv"
	"strings"
)

// Port of pi v1.5.0 packages/schema/src/explain.ts. Paths are JSON Pointers
// while walking and are formatted to the dotted path the model sees by
// formatValidationPath.

type piError struct {
	path    string
	message string
}

func (s *piSchema) errors(value *schemaValue) []piError {
	var out []piError
	s.walkErrors(value, "", &out)
	if len(out) == 0 {
		out = append(out, piError{message: "invalid"})
	}
	return out
}

func (s *piSchema) walkErrors(value *schemaValue, path string, out *[]piError) {
	if s == nil {
		return
	}
	if s.false {
		addPiError(out, path, "no value is allowed here")
		return
	}
	if s.check(value) {
		return
	}
	start := len(*out)
	push := func(message string) { addPiError(out, path, message) }

	if len(s.types) > 0 {
		matched := false
		for _, typ := range s.types {
			if matchesJSONType(value, typ) {
				matched = true
			}
		}
		if !matched {
			push("expected " + strings.Join(s.types, " or ") + ", got " + schemaTypeName(value))
			return
		}
	}
	if s.hasEnum && !schemaLiteralIn(value, s.enum) {
		push("expected one of " + schemaListValues(s.enum))
	}
	if s.hasConst && !schemaLiteralEqual(value, s.constVal) {
		push("expected " + string(encodeSchemaValue(s.constVal)))
	}
	if value.IsString {
		s.walkString(value.String, push)
	}
	if value.IsNumber {
		s.walkNumber(value.Number, push)
	}
	if value.Object != nil {
		s.walkObject(value, path, out, push)
	}
	if value.IsArray {
		s.walkArray(value, path, out, push)
	}
	for _, sub := range s.allOf {
		sub.walkErrors(value, path, out)
	}
	if s.anyOf != nil {
		s.walkUnion(s.anyOf, value, path, out, false)
	}
	if s.oneOf != nil {
		s.walkUnion(s.oneOf, value, path, out, true)
	}
	if s.not != nil && s.not.check(value) {
		push("must not match the schema in not")
	}
	if s.ifS != nil {
		branch := s.elseS
		if s.ifS.check(value) {
			branch = s.thenS
		}
		if branch != nil {
			branch.walkErrors(value, path, out)
		}
	}
	if s.ref != "" {
		target, ok := s.resolve()
		if !ok {
			addPiError(out, path, "unresolved reference "+s.ref)
		} else if target != nil {
			target.walkErrors(value, path, out)
		}
	}
	if len(*out) == start && (s.unevaluatedFalse || s.unevaluatedProperties != nil) && value.Object != nil {
		annotations := s.annotations(value)
		if !annotations.allProps {
			for _, key := range value.Object.keys {
				if annotations.props[key] {
					continue
				}
				field, _ := value.Object.get(key)
				if s.unevaluatedFalse {
					addPiError(out, pointerTo(path, key), "unknown field")
				} else if s.unevaluatedProperties != nil {
					s.unevaluatedProperties.walkErrors(field, pointerTo(path, key), out)
				}
			}
		}
	}
	if len(*out) == start && (s.unevaluatedItemsFalse || s.unevaluatedItems != nil) && value.IsArray {
		annotations := s.annotations(value)
		if !annotations.allItems {
			for i := annotations.items; i < len(value.Array); i++ {
				if annotations.indexes[i] {
					continue
				}
				if s.unevaluatedItemsFalse {
					addPiError(out, path+"/"+strconv.Itoa(i), "unexpected item")
				} else if s.unevaluatedItems != nil {
					s.unevaluatedItems.walkErrors(value.Array[i], path+"/"+strconv.Itoa(i), out)
				}
			}
		}
	}
	if len(*out) == start && s.dynamicRef != "" {
		push("does not match the dynamically referenced schema")
	}
}

func (s *piSchema) walkString(value string, push func(string)) {
	n := codePointCount(value)
	if s.minLength != nil && n < *s.minLength {
		push("expected at least " + strconv.Itoa(*s.minLength) + " characters")
	}
	if s.maxLength != nil && n > *s.maxLength {
		push("expected at most " + strconv.Itoa(*s.maxLength) + " characters")
	}
	if s.patternRe != nil && !s.patternRe.MatchString(value) {
		push("expected to match " + s.pattern)
	}
	if s.format != "" && !piFormatValid(s.format, value) {
		push("expected format " + s.format)
	}
}

func (s *piSchema) walkNumber(value float64, push func(string)) {
	if s.minimum != nil && value < *s.minimum {
		push("expected >= " + JSNumberToString(*s.minimum))
	}
	if s.maximum != nil && value > *s.maximum {
		push("expected <= " + JSNumberToString(*s.maximum))
	}
	if s.exclusiveMinimum != nil && value <= *s.exclusiveMinimum {
		push("expected > " + JSNumberToString(*s.exclusiveMinimum))
	}
	if s.exclusiveMaximum != nil && value >= *s.exclusiveMaximum {
		push("expected < " + JSNumberToString(*s.exclusiveMaximum))
	}
	if s.multipleOf != nil && !isMultipleOf(value, *s.multipleOf) {
		push("expected a multiple of " + JSNumberToString(*s.multipleOf))
	}
}

func (s *piSchema) walkObject(value *schemaValue, path string, out *[]piError, push func(string)) {
	for _, key := range s.required {
		if _, ok := value.Object.get(key); !ok {
			addPiError(out, pointerTo(path, key), "missing")
		}
	}
	for _, key := range s.propertyOrder {
		if field, ok := value.Object.get(key); ok {
			s.properties[key].walkErrors(field, pointerTo(path, key), out)
		}
	}
	for _, key := range value.Object.keys {
		field, _ := value.Object.get(key)
		declared := false
		if _, ok := s.properties[key]; ok {
			declared = true
		}
		for _, pattern := range s.patternOrder {
			re := s.patternRes[pattern]
			if re != nil && re.MatchString(key) {
				declared = true
				s.patternProps[pattern].walkErrors(field, pointerTo(path, key), out)
			}
		}
		if !declared {
			if s.additionalFalse {
				addPiError(out, pointerTo(path, key), "unknown field")
			} else if s.additionalProps != nil {
				s.additionalProps.walkErrors(field, pointerTo(path, key), out)
			}
		}
		if s.propertyNames != nil {
			name := &schemaValue{IsString: true, String: key}
			if !s.propertyNames.check(name) {
				addPiError(out, pointerTo(path, key), "invalid property name")
			}
		}
	}
	count := len(value.Object.keys)
	if s.minProperties != nil && count < *s.minProperties {
		push("expected at least " + strconv.Itoa(*s.minProperties) + " properties")
	}
	if s.maxProperties != nil && count > *s.maxProperties {
		push("expected at most " + strconv.Itoa(*s.maxProperties) + " properties")
	}
	for key, list := range s.dependentReq {
		if _, ok := value.Object.get(key); !ok {
			continue
		}
		for _, needed := range list {
			if _, ok := value.Object.get(needed); !ok {
				addPiError(out, pointerTo(path, needed), "required when "+key+" is present")
			}
		}
	}
	for key, list := range s.dependencyLists {
		if _, ok := value.Object.get(key); !ok {
			continue
		}
		for _, needed := range list {
			if _, ok := value.Object.get(needed); !ok {
				addPiError(out, pointerTo(path, needed), "required when "+key+" is present")
			}
		}
	}
	for key, dep := range s.dependentSchemas {
		if _, ok := value.Object.get(key); ok {
			dep.walkErrors(value, path, out)
		}
	}
	for key, dep := range s.dependencies {
		if _, ok := value.Object.get(key); ok {
			dep.walkErrors(value, path, out)
		}
	}
}

func (s *piSchema) walkArray(value *schemaValue, path string, out *[]piError, push func(string)) {
	for i, item := range value.Array {
		if schema := s.itemAt(i); schema != nil {
			schema.walkErrors(item, path+"/"+strconv.Itoa(i), out)
		}
	}
	if s.minItems != nil && len(value.Array) < *s.minItems {
		push("expected at least " + strconv.Itoa(*s.minItems) + " items")
	}
	if s.maxItems != nil && len(value.Array) > *s.maxItems {
		push("expected at most " + strconv.Itoa(*s.maxItems) + " items")
	}
	if s.uniqueItems && !uniqueSchemaValues(value.Array) {
		push("expected unique items")
	}
	if s.contains != nil {
		minimum := 1
		if s.minContains != nil {
			minimum = *s.minContains
		}
		maximum := math.MaxInt
		if s.maxContains != nil {
			maximum = *s.maxContains
		}
		count := 0
		for _, item := range value.Array {
			if s.contains.check(item) {
				count++
			}
		}
		if count < minimum || count > maximum {
			push(containsMessage(minimum, maximum))
		}
	}
}

func (s *piSchema) walkUnion(branches []*piSchema, value *schemaValue, path string, out *[]piError, exactlyOne bool) {
	matches := 0
	for _, branch := range branches {
		if branch.check(value) {
			matches++
		}
	}
	if matches == 1 || (!exactlyOne && matches > 0) {
		return
	}
	if matches > 1 {
		addPiError(out, path, "matches more than one alternative")
		return
	}
	if key, tags, ok := piUnionTag(branches); ok && value.Object != nil {
		actual, exists := value.Object.get(key)
		if !exists || !schemaLiteralIn(actual, tags) {
			addPiError(out, pointerTo(path, key), "expected one of "+schemaListValues(tags))
			return
		}
		for i, tag := range tags {
			if schemaLiteralEqual(actual, tag) {
				branches[i].walkErrors(value, path, out)
				return
			}
		}
	}
	var best []piError
	bestDeclared := -1
	for _, branch := range branches {
		var errs []piError
		branch.walkErrors(value, path, &errs)
		if len(errs) == 0 {
			continue
		}
		declared := 0
		if value.Object != nil {
			declared = branch.declaredKeys(value)
		}
		if best == nil || declared > bestDeclared || (declared == bestDeclared && len(errs[0].path) > len(best[0].path)) {
			best = errs
			bestDeclared = declared
		}
	}
	if best != nil {
		*out = append(*out, best...)
	} else {
		addPiError(out, path, "matches no alternative")
	}
}

func (s *piSchema) declaredKeys(value *schemaValue) int {
	target := s
	if s.ref != "" {
		if resolved, ok := s.resolve(); ok {
			target = resolved
		}
	}
	count := 0
	for _, key := range value.Object.keys {
		if _, ok := target.properties[key]; ok {
			count++
		}
	}
	return count
}

func piUnionTag(branches []*piSchema) (string, []*schemaValue, bool) {
	if len(branches) < 2 {
		return "", nil, false
	}
	resolved := make([]*piSchema, 0, len(branches))
	for _, branch := range branches {
		target := branch
		if branch.ref != "" {
			if next, ok := branch.resolve(); ok {
				target = next
			}
		}
		resolved = append(resolved, target)
	}
	if len(resolved[0].propertyOrder) == 0 {
		return "", nil, false
	}
	for _, key := range resolved[0].propertyOrder {
		var tags []*schemaValue
		for _, branch := range resolved {
			prop := branch.properties[key]
			if prop == nil || !prop.hasConst || prop.constVal == nil || prop.constVal.Object != nil || prop.constVal.IsArray {
				tags = nil
				break
			}
			tags = append(tags, prop.constVal)
		}
		if len(tags) != len(branches) {
			continue
		}
		seen := false
		for i := range tags {
			for j := range i {
				if schemaLiteralEqual(tags[i], tags[j]) {
					seen = true
				}
			}
		}
		if !seen {
			return key, tags, true
		}
	}
	return "", nil, false
}

func addPiError(out *[]piError, path, message string) {
	*out = append(*out, piError{path: path, message: message})
}

func pointerTo(path, key string) string {
	return path + "/" + escapeJSONPointer(key)
}

func containsMessage(minimum, maximum int) string {
	if maximum == math.MaxInt {
		if minimum == 1 {
			return "expected at least 1 matching item"
		}
		return "expected at least " + strconv.Itoa(minimum) + " matching items"
	}
	if minimum == 0 {
		if maximum == 1 {
			return "expected at most 1 matching item"
		}
		return "expected at most " + strconv.Itoa(maximum) + " matching items"
	}
	return "expected between " + strconv.Itoa(minimum) + " and " + strconv.Itoa(maximum) + " matching items"
}

// formatValidationPath turns a JSON Pointer into the dotted path the model
// sees: /edits/0/oldText -> edits.0.oldText.
func formatValidationPath(pointer string) string {
	if pointer == "" {
		return "root"
	}
	parts := strings.Split(pointer[1:], "/")
	for i, part := range parts {
		part = strings.ReplaceAll(strings.ReplaceAll(part, "~1", "/"), "~0", "~")
		parts[i] = part
	}
	return strings.Join(parts, ".")
}

func schemaListValues(values []*schemaValue) string {
	const limit = 20
	shown := values
	if len(shown) > limit {
		shown = shown[:limit]
	}
	var b strings.Builder
	b.WriteByte('[')
	for i, value := range shown {
		if i > 0 {
			b.WriteByte(',')
		}
		b.Write(encodeSchemaValue(value))
	}
	b.WriteByte(']')
	if len(values) > limit {
		text := b.String()
		return text[:len(text)-1] + ", ... and " + strconv.Itoa(len(values)-limit) + " more]"
	}
	return b.String()
}

func schemaTypeName(value *schemaValue) string {
	switch {
	case value.Null:
		return "null"
	case value.IsUndefined:
		return "undefined"
	case value.IsArray:
		return "array"
	case value.Object != nil:
		return "object"
	case value.IsString:
		return "string"
	case value.IsBool:
		return "boolean"
	case value.IsBigInt:
		return "bigint"
	case value.IsNumber:
		if math.IsNaN(value.Number) {
			return "NaN"
		}
		if math.IsInf(value.Number, 1) {
			return "Infinity"
		}
		if math.IsInf(value.Number, -1) {
			return "-Infinity"
		}
		if value.Number == math.Trunc(value.Number) {
			return "integer"
		}
		return "number"
	default:
		return "unknown"
	}
}
