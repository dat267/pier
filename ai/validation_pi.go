package ai

import (
	"encoding/json"
	"fmt"
	"math"
	"net"
	"net/mail"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Port of pi v1.5.0's @earendil-works/pi-schema validation and coercion as
// used by ai.validateToolArguments. The reference is f9a26365
// packages/schema/src/{compiler,checks,coerce,explain,resolver}.ts. Schemas
// arrive as JSON, so the port keeps an ordered value tree and resolves local
// references lazily.

type piSchema struct {
	doc     *piDoc
	pointer string
	false   bool

	types []string

	properties    map[string]*piSchema
	propertyOrder []string
	required      []string
	requiredSet   map[string]bool

	items            []*piSchema
	itemsOne         *piSchema
	prefixItems      []*piSchema
	additionalItems  *piSchema
	additionalProps  *piSchema
	additionalFalse  bool
	patternProps     map[string]*piSchema
	patternOrder     []string
	patternRes       map[string]*regexp.Regexp
	propertyNames    *piSchema
	minProperties    *int
	maxProperties    *int
	dependentReq     map[string][]string
	dependentSchemas map[string]*piSchema
	dependencies     map[string]*piSchema
	dependencyLists  map[string][]string

	minItems    *int
	maxItems    *int
	uniqueItems bool
	contains    *piSchema
	minContains *int
	maxContains *int

	pattern   string
	patternRe *regexp.Regexp
	format    string

	minLength *int
	maxLength *int

	minimum          *float64
	maximum          *float64
	exclusiveMinimum *float64
	exclusiveMaximum *float64
	multipleOf       *float64

	enum     []*schemaValue
	hasEnum  bool
	hasConst bool
	constVal *schemaValue

	allOf []*piSchema
	anyOf []*piSchema
	oneOf []*piSchema
	not   *piSchema
	ifS   *piSchema
	thenS *piSchema
	elseS *piSchema

	ref string

	unevaluatedProperties *piSchema
	unevaluatedFalse      bool
	unevaluatedItems      *piSchema
	unevaluatedItemsFalse bool
}

type piDoc struct {
	root       *schemaValue
	cache      map[string]*piSchema
	ids        map[string]string
	anchors    map[string]string
	rootSchema *piSchema
	err        error
}

func parsePiSchema(data json.RawMessage) (*piDoc, *piSchema, error) {
	root := decodeSchemaValue(data)
	if root == nil {
		root = &schemaValue{}
	}
	doc := &piDoc{
		root:    root,
		cache:   map[string]*piSchema{},
		ids:     map[string]string{},
		anchors: map[string]string{},
	}
	doc.indexIDs(root, "")
	doc.rootSchema = doc.node("")
	doc.prewarmRefs(doc.rootSchema, map[*piSchema]bool{})
	return doc, doc.rootSchema, doc.err
}

// prewarmRefs resolves every reference while the doc is still single-threaded.
// The cached schema nodes are immutable afterward, so concurrent tool calls can
// follow refs without touching the cache.
func (d *piDoc) prewarmRefs(schema *piSchema, seen map[*piSchema]bool) {
	if schema == nil || seen[schema] {
		return
	}
	seen[schema] = true
	if schema.ref != "" {
		if target, ok := schema.resolve(); ok && target != nil {
			d.prewarmRefs(target, seen)
		}
	}
	for _, sub := range schema.properties {
		d.prewarmRefs(sub, seen)
	}
	for _, sub := range schema.patternProps {
		d.prewarmRefs(sub, seen)
	}
	for _, list := range [][]*piSchema{schema.items, schema.prefixItems, schema.allOf, schema.anyOf, schema.oneOf} {
		for _, sub := range list {
			d.prewarmRefs(sub, seen)
		}
	}
	for _, sub := range []*piSchema{
		schema.itemsOne, schema.additionalItems, schema.additionalProps,
		schema.propertyNames, schema.contains, schema.not, schema.ifS,
		schema.thenS, schema.elseS, schema.unevaluatedProperties,
		schema.unevaluatedItems,
	} {
		d.prewarmRefs(sub, seen)
	}
	for _, sub := range schema.dependentSchemas {
		d.prewarmRefs(sub, seen)
	}
	for _, sub := range schema.dependencies {
		d.prewarmRefs(sub, seen)
	}
}

func (d *piDoc) indexIDs(v *schemaValue, pointer string) {
	if v == nil || v.Object == nil {
		if v != nil && v.IsArray {
			for i, item := range v.Array {
				d.indexIDs(item, pointer+"/"+strconv.Itoa(i))
			}
		}
		return
	}
	if id := svString(v, "$id"); id != "" {
		d.ids[id] = pointer
	}
	if anchor := svString(v, "$anchor"); anchor != "" {
		d.anchors["#"+anchor] = pointer
	}
	if anchor := svString(v, "$dynamicAnchor"); anchor != "" {
		d.anchors["#"+anchor] = pointer
	}
	for _, key := range v.Object.keys {
		d.indexIDs(v.Object.values[key], pointer+"/"+escapeJSONPointer(key))
	}
}

func (d *piDoc) node(pointer string) *piSchema {
	if node, ok := d.cache[pointer]; ok {
		return node
	}
	node := &piSchema{doc: d, pointer: pointer}
	d.cache[pointer] = node
	value := valueAtPointer(d.root, pointer)
	if value == nil {
		node.false = true
		return node
	}
	if value.IsBool {
		node.false = !value.Bool
		return node
	}
	if value.Object == nil {
		node.false = true
		if d.err == nil {
			d.err = fmt.Errorf("A schema must be an object or a boolean")
		}
		return node
	}
	node.parse(value)
	return node
}

func (s *piSchema) parse(v *schemaValue) {
	if svBool(v, "type") != nil {
		// handled below
	}
	if typ := svProp(v, "type"); typ != nil {
		if typ.IsString {
			s.types = []string{typ.String}
		} else if typ.IsArray {
			for _, item := range typ.Array {
				if item.IsString {
					s.types = append(s.types, item.String)
				}
			}
		}
	}
	if props := svProp(v, "properties"); props != nil && props.Object != nil {
		s.properties = map[string]*piSchema{}
		for _, key := range props.Object.keys {
			s.properties[key] = s.doc.node(s.pointer + "/properties/" + escapeJSONPointer(key))
			s.propertyOrder = append(s.propertyOrder, key)
		}
	}
	if required := svProp(v, "required"); required != nil && required.IsArray {
		s.requiredSet = map[string]bool{}
		for _, item := range required.Array {
			if item.IsString {
				s.required = append(s.required, item.String)
				s.requiredSet[item.String] = true
			}
		}
	}
	if items := svProp(v, "items"); items != nil && !items.Null {
		if items.IsArray {
			for i := range items.Array {
				s.items = append(s.items, s.doc.node(s.pointer+"/items/"+strconv.Itoa(i)))
			}
		} else {
			s.itemsOne = s.doc.node(s.pointer + "/items")
		}
	}
	if prefix := svProp(v, "prefixItems"); prefix != nil && prefix.IsArray {
		for i := range prefix.Array {
			s.prefixItems = append(s.prefixItems, s.doc.node(s.pointer+"/prefixItems/"+strconv.Itoa(i)))
		}
	}
	if add := svProp(v, "additionalItems"); add != nil {
		if add.IsBool && !add.Bool {
			s.additionalItems = &piSchema{doc: s.doc, false: true}
		} else if !(add.IsBool && add.Bool) {
			s.additionalItems = s.doc.node(s.pointer + "/additionalItems")
		}
	}
	if add := svProp(v, "additionalProperties"); add != nil {
		if add.IsBool {
			s.additionalFalse = !add.Bool
		} else {
			s.additionalProps = s.doc.node(s.pointer + "/additionalProperties")
		}
	}
	if patterns := svProp(v, "patternProperties"); patterns != nil && patterns.Object != nil {
		s.patternProps = map[string]*piSchema{}
		s.patternRes = map[string]*regexp.Regexp{}
		for _, key := range patterns.Object.keys {
			s.patternOrder = append(s.patternOrder, key)
			s.patternProps[key] = s.doc.node(s.pointer + "/patternProperties/" + escapeJSONPointer(key))
			if re, err := regexp.Compile(key); err == nil {
				s.patternRes[key] = re
			}
		}
	}
	if pn := svProp(v, "propertyNames"); pn != nil {
		s.propertyNames = s.doc.node(s.pointer + "/propertyNames")
	}
	s.minProperties = svInt(v, "minProperties")
	s.maxProperties = svInt(v, "maxProperties")
	if dep := svProp(v, "dependentRequired"); dep != nil && dep.Object != nil {
		s.dependentReq = map[string][]string{}
		for _, key := range dep.Object.keys {
			if list := dep.Object.values[key]; list != nil && list.IsArray {
				for _, item := range list.Array {
					if item.IsString {
						s.dependentReq[key] = append(s.dependentReq[key], item.String)
					}
				}
			}
		}
	}
	if dep := svProp(v, "dependentSchemas"); dep != nil && dep.Object != nil {
		s.dependentSchemas = map[string]*piSchema{}
		for _, key := range dep.Object.keys {
			s.dependentSchemas[key] = s.doc.node(s.pointer + "/dependentSchemas/" + escapeJSONPointer(key))
		}
	}
	if deps := svProp(v, "dependencies"); deps != nil && deps.Object != nil {
		s.dependencies = map[string]*piSchema{}
		s.dependencyLists = map[string][]string{}
		for _, key := range deps.Object.keys {
			value := deps.Object.values[key]
			if value.IsArray {
				for _, item := range value.Array {
					if item.IsString {
						s.dependencyLists[key] = append(s.dependencyLists[key], item.String)
					}
				}
			} else if value.Object != nil || value.IsBool {
				s.dependencies[key] = s.doc.node(s.pointer + "/dependencies/" + escapeJSONPointer(key))
			}
		}
	}
	s.minItems = svInt(v, "minItems")
	s.maxItems = svInt(v, "maxItems")
	s.uniqueItems = svBool(v, "uniqueItems") != nil && *svBool(v, "uniqueItems")
	if contains := svProp(v, "contains"); contains != nil {
		s.contains = s.doc.node(s.pointer + "/contains")
	}
	s.minContains = svInt(v, "minContains")
	s.maxContains = svInt(v, "maxContains")
	if pattern := svString(v, "pattern"); pattern != "" {
		s.pattern = pattern
		re, err := regexp.Compile(pattern)
		if err != nil {
			// Go's RE2 lacks a few constructs ECMAScript accepts (lookbehind,
			// backreferences). Keep those unconstrained instead of treating a
			// valid JavaScript pattern as a schema error.
			if !strings.Contains(err.Error(), "invalid or unsupported Perl syntax") && !strings.Contains(err.Error(), "invalid escape sequence") && !strings.Contains(err.Error(), "invalid named capture") {
				if s.doc.err == nil {
					message := err.Error()
					if strings.Contains(message, "missing closing ]") {
						message = "Unterminated character class"
					}
					s.doc.err = fmt.Errorf("Invalid regular expression: /%s/: %s", pattern, message)
				}
			}
		} else {
			s.patternRe = re
		}
	}
	s.format = svString(v, "format")
	s.minLength = svInt(v, "minLength")
	s.maxLength = svInt(v, "maxLength")
	s.minimum = svFloat(v, "minimum")
	s.maximum = svFloat(v, "maximum")
	s.exclusiveMinimum = svFloat(v, "exclusiveMinimum")
	s.exclusiveMaximum = svFloat(v, "exclusiveMaximum")
	s.multipleOf = svFloat(v, "multipleOf")
	if enum := svProp(v, "enum"); enum != nil && enum.IsArray {
		s.hasEnum = true
		for _, item := range enum.Array {
			s.enum = append(s.enum, item)
		}
	}
	if c := svProp(v, "const"); c != nil {
		s.hasConst = true
		s.constVal = c
	}
	s.allOf = s.parseList(v, "allOf")
	s.anyOf = s.parseList(v, "anyOf")
	s.oneOf = s.parseList(v, "oneOf")
	if n := svProp(v, "not"); n != nil {
		s.not = s.doc.node(s.pointer + "/not")
	}
	if c := svProp(v, "if"); c != nil {
		s.ifS = s.doc.node(s.pointer + "/if")
	}
	if c := svProp(v, "then"); c != nil {
		s.thenS = s.doc.node(s.pointer + "/then")
	}
	if c := svProp(v, "else"); c != nil {
		s.elseS = s.doc.node(s.pointer + "/else")
	}
	s.ref = svString(v, "$ref")
	if u := svProp(v, "unevaluatedProperties"); u != nil {
		if u.IsBool {
			s.unevaluatedFalse = !u.Bool
		} else {
			s.unevaluatedProperties = s.doc.node(s.pointer + "/unevaluatedProperties")
		}
	}
	if u := svProp(v, "unevaluatedItems"); u != nil {
		if u.IsBool {
			s.unevaluatedItemsFalse = !u.Bool
		} else {
			s.unevaluatedItems = s.doc.node(s.pointer + "/unevaluatedItems")
		}
	}
}

func (s *piSchema) parseList(v *schemaValue, key string) []*piSchema {
	list := svProp(v, key)
	if list == nil || !list.IsArray {
		return nil
	}
	out := make([]*piSchema, 0, len(list.Array))
	for i := range list.Array {
		out = append(out, s.doc.node(s.pointer+"/"+key+"/"+strconv.Itoa(i)))
	}
	return out
}

func (s *piSchema) resolve() (*piSchema, bool) {
	if s.ref == "" {
		return nil, false
	}
	ref := s.ref
	if strings.HasPrefix(ref, "#") {
		frag := ref[1:]
		if unescaped, err := url.PathUnescape(frag); err == nil {
			frag = unescaped
		}
		if frag == "" {
			return s.doc.rootSchema, true
		}
		if strings.HasPrefix(frag, "/") {
			if valueAtPointer(s.doc.root, frag) == nil {
				return nil, false
			}
			return s.doc.node(frag), true
		}
		if pointer, ok := s.doc.anchors["#"+frag]; ok {
			return s.doc.node(pointer), true
		}
		return nil, false
	}
	if pointer, ok := s.doc.ids[ref]; ok {
		return s.doc.node(pointer), true
	}
	return nil, false
}

func (s *piSchema) check(value *schemaValue) bool {
	return s.checkActive(value, map[piCheckVisit]bool{})
}

type piCheckVisit struct {
	schema *piSchema
	value  *schemaValue
}

func (s *piSchema) checkActive(value *schemaValue, active map[piCheckVisit]bool) bool {
	if s == nil || value == nil {
		return true
	}
	if s.false {
		return false
	}
	visit := piCheckVisit{s, value}
	if active[visit] {
		return true
	}
	active[visit] = true
	defer delete(active, visit)

	if s.ref != "" {
		target, ok := s.resolve()
		if !ok {
			return false
		}
		if target == nil || !target.checkActive(value, active) {
			return false
		}
	}
	if len(s.types) > 0 {
		matched := false
		for _, typ := range s.types {
			if matchesJSONType(value, typ) {
				matched = true
			}
		}
		if !matched {
			return false
		}
	}
	if s.hasConst && !schemaLiteralEqual(value, s.constVal) {
		return false
	}
	if s.hasEnum {
		matched := false
		for _, option := range s.enum {
			if schemaLiteralEqual(value, option) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	if value.IsString {
		if s.minLength != nil && codePointCount(value.String) < *s.minLength {
			return false
		}
		if s.maxLength != nil && codePointCount(value.String) > *s.maxLength {
			return false
		}
		if s.patternRe != nil && !s.patternRe.MatchString(value.String) {
			return false
		}
		if s.format != "" && !piFormatValid(s.format, value.String) {
			return false
		}
	}
	if value.IsNumber {
		if s.minimum != nil && value.Number < *s.minimum {
			return false
		}
		if s.maximum != nil && value.Number > *s.maximum {
			return false
		}
		if s.exclusiveMinimum != nil && value.Number <= *s.exclusiveMinimum {
			return false
		}
		if s.exclusiveMaximum != nil && value.Number >= *s.exclusiveMaximum {
			return false
		}
		if s.multipleOf != nil && !isMultipleOf(value.Number, *s.multipleOf) {
			return false
		}
	}
	if value.Object != nil && s.hasObjectKeywords() {
		for _, key := range s.required {
			if _, ok := value.Object.get(key); !ok {
				return false
			}
		}
		for _, key := range s.propertyOrder {
			if field, ok := value.Object.get(key); ok {
				if !s.properties[key].checkActive(field, active) {
					return false
				}
			}
		}
		for _, key := range value.Object.keys {
			field, _ := value.Object.get(key)
			matched := false
			if _, ok := s.properties[key]; ok {
				matched = true
			}
			for _, pattern := range s.patternOrder {
				re := s.patternRes[pattern]
				if re != nil && re.MatchString(key) {
					matched = true
					if !s.patternProps[pattern].checkActive(field, active) {
						return false
					}
				}
			}
			if !matched {
				if s.additionalFalse {
					return false
				}
				if s.additionalProps != nil && !s.additionalProps.checkActive(field, active) {
					return false
				}
			}
			if s.propertyNames != nil {
				name := &schemaValue{IsString: true, String: key}
				if !s.propertyNames.checkActive(name, active) {
					return false
				}
			}
		}
		if s.minProperties != nil {
			count := len(value.Object.keys)
			if count < *s.minProperties {
				return false
			}
		}
		if s.maxProperties != nil {
			count := len(value.Object.keys)
			if count > *s.maxProperties {
				return false
			}
		}
		for key, list := range s.dependentReq {
			if _, ok := value.Object.get(key); !ok {
				continue
			}
			for _, needed := range list {
				if _, ok := value.Object.get(needed); !ok {
					return false
				}
			}
		}
		for key, dep := range s.dependentSchemas {
			if _, ok := value.Object.get(key); ok && !dep.checkActive(value, active) {
				return false
			}
		}
		for key, list := range s.dependencyLists {
			if _, ok := value.Object.get(key); !ok {
				continue
			}
			for _, needed := range list {
				if _, ok := value.Object.get(needed); !ok {
					return false
				}
			}
		}
		for key, dep := range s.dependencies {
			if _, ok := value.Object.get(key); ok && !dep.checkActive(value, active) {
				return false
			}
		}
	}
	if value.IsArray && s.hasArrayKeywords() {
		for i, item := range value.Array {
			schema := s.itemAt(i)
			if schema != nil && !schema.checkActive(item, active) {
				return false
			}
		}
		if s.minItems != nil && len(value.Array) < *s.minItems {
			return false
		}
		if s.maxItems != nil && len(value.Array) > *s.maxItems {
			return false
		}
		if s.uniqueItems && !uniqueSchemaValues(value.Array) {
			return false
		}
		if s.contains != nil {
			count := 0
			for _, item := range value.Array {
				if s.contains.checkActive(item, active) {
					count++
				}
			}
			min := 1
			if s.minContains != nil {
				min = *s.minContains
			}
			max := math.MaxInt
			if s.maxContains != nil {
				max = *s.maxContains
			}
			if count < min || count > max {
				return false
			}
		}
	}
	if value.Object != nil && (s.unevaluatedFalse || s.unevaluatedProperties != nil) {
		evaluated, all := s.evaluatedProps(value)
		if !all {
			for _, key := range value.Object.keys {
				if evaluated[key] {
					continue
				}
				if s.unevaluatedFalse {
					return false
				}
				field, _ := value.Object.get(key)
				if !s.unevaluatedProperties.checkActive(field, active) {
					return false
				}
			}
		}
	}
	for _, sub := range s.allOf {
		if !sub.checkActive(value, active) {
			return false
		}
	}
	if s.anyOf != nil {
		matched := false
		for _, sub := range s.anyOf {
			if sub.checkActive(value, active) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	if s.oneOf != nil {
		matches := 0
		for _, sub := range s.oneOf {
			if sub.checkActive(value, active) {
				matches++
			}
		}
		if matches != 1 {
			return false
		}
	}
	if s.not != nil && s.not.checkActive(value, active) {
		return false
	}
	if s.ifS != nil {
		if s.ifS.checkActive(value, active) {
			if s.thenS != nil && !s.thenS.checkActive(value, active) {
				return false
			}
		} else if s.elseS != nil && !s.elseS.checkActive(value, active) {
			return false
		}
	}
	return true
}

func (s *piSchema) evaluatedProps(value *schemaValue) (map[string]bool, bool) {
	evaluated := map[string]bool{}
	var add func(*piSchema)
	add = func(schema *piSchema) {
		if schema == nil || schema.false || value.Object == nil {
			return
		}
		if schema.ref != "" {
			if target, ok := schema.resolve(); ok {
				add(target)
			}
		}
		for _, key := range schema.propertyOrder {
			if _, ok := value.Object.get(key); ok {
				evaluated[key] = true
			}
		}
		for _, key := range value.Object.keys {
			for _, pattern := range schema.patternOrder {
				if re := schema.patternRes[pattern]; re != nil && re.MatchString(key) {
					evaluated[key] = true
				}
			}
		}
		if schema.additionalProps != nil || schema.additionalFalse {
			for _, key := range value.Object.keys {
				_, declared := schema.properties[key]
				matched := declared
				for _, pattern := range schema.patternOrder {
					if re := schema.patternRes[pattern]; re != nil && re.MatchString(key) {
						matched = true
					}
				}
				if !matched {
					evaluated[key] = true
				}
			}
		}
		for _, sub := range schema.allOf {
			add(sub)
		}
		for _, branches := range [][]*piSchema{schema.anyOf, schema.oneOf} {
			for _, branch := range branches {
				if branch.check(value) {
					add(branch)
				}
			}
		}
		if schema.ifS != nil {
			if schema.ifS.check(value) {
				add(schema.ifS)
				add(schema.thenS)
			} else {
				add(schema.elseS)
			}
		}
		for key, dep := range schema.dependentSchemas {
			if _, ok := value.Object.get(key); ok {
				add(dep)
			}
		}
		for key, dep := range schema.dependencies {
			if _, ok := value.Object.get(key); ok {
				add(dep)
			}
		}
	}
	add(s)
	return evaluated, false
}

func (s *piSchema) hasObjectKeywords() bool {
	return len(s.properties) > 0 || len(s.required) > 0 || len(s.patternProps) > 0 || s.additionalProps != nil || s.additionalFalse || s.propertyNames != nil || s.minProperties != nil || s.maxProperties != nil || len(s.dependentReq) > 0 || len(s.dependentSchemas) > 0 || len(s.dependencies) > 0 || len(s.dependencyLists) > 0
}

func (s *piSchema) hasArrayKeywords() bool {
	return s.itemsOne != nil || len(s.items) > 0 || len(s.prefixItems) > 0 || s.additionalItems != nil || s.minItems != nil || s.maxItems != nil || s.uniqueItems || s.contains != nil
}

func (s *piSchema) itemAt(index int) *piSchema {
	if index < len(s.prefixItems) {
		return s.prefixItems[index]
	}
	if index < len(s.items) {
		return s.items[index]
	}
	if len(s.prefixItems) > 0 {
		return s.itemsOne
	}
	if len(s.items) > 0 {
		return s.additionalItems
	}
	return s.itemsOne
}

func isFiniteJSONNumber(n float64) bool { return !math.IsInf(n, 0) && !math.IsNaN(n) }

func codePointCount(s string) int { return len([]rune(s)) }

func uniqueSchemaValues(values []*schemaValue) bool {
	for i := 1; i < len(values); i++ {
		for j := 0; j < i; j++ {
			if schemaLiteralEqual(values[i], values[j]) {
				return false
			}
		}
	}
	return true
}

func isMultipleOf(value, divisor float64) bool {
	if divisor == 0 {
		return false
	}
	q := value / divisor
	if q == math.Trunc(q) {
		return true
	}
	if !isFiniteJSONNumber(q) {
		return math.Mod(value, divisor) == 0
	}
	// Compare the decimal spellings so 0.07 / 0.01 is exact.
	return decimalMultiple(value, divisor)
}

func decimalMultiple(value, divisor float64) bool {
	vs := strconv.FormatFloat(math.Abs(value), 'g', -1, 64)
	ds := strconv.FormatFloat(math.Abs(divisor), 'g', -1, 64)
	vm, ve := decimalParts(vs)
	dm, de := decimalParts(ds)
	if ve >= de {
		pow := intPow10(ve - de)
		return (vm*pow)%dm == 0
	}
	return vm%(dm*intPow10(de-ve)) == 0
}

func decimalParts(s string) (int64, int) {
	mantissa, exp := s, 0
	if i := strings.IndexAny(s, "eE"); i >= 0 {
		mantissa = s[:i]
		exp, _ = strconv.Atoi(s[i+1:])
	}
	point := strings.IndexByte(mantissa, '.')
	fraction := 0
	if point >= 0 {
		fraction = len(mantissa) - point - 1
		mantissa = mantissa[:point] + mantissa[point+1:]
	}
	n, _ := strconv.ParseInt(mantissa, 10, 64)
	return n, exp - fraction
}

func intPow10(n int) int64 {
	out := int64(1)
	for range n {
		out *= 10
	}
	return out
}

func piFormatValid(name, value string) bool {
	switch name {
	case "email":
		addr, err := mail.ParseAddress(value)
		return err == nil && addr.Address == value && strings.Contains(value, "@")
	case "uri":
		u, err := url.Parse(value)
		return err == nil && u.IsAbs()
	case "url":
		u, err := url.Parse(value)
		return err == nil && u.Scheme != "" && u.Host != ""
	case "uri-reference":
		_, err := url.Parse(value)
		return err == nil
	case "hostname":
		return validHostname(value)
	case "ipv4":
		ip := net.ParseIP(value)
		return ip != nil && ip.To4() != nil && !strings.Contains(value, ":")
	case "ipv6":
		ip := net.ParseIP(value)
		return ip != nil && ip.To4() == nil && strings.Contains(value, ":")
	case "uuid":
		matched, _ := regexp.MatchString(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`, value)
		return matched
	case "date":
		_, err := time.Parse("2006-01-02", value)
		return err == nil
	case "date-time":
		normalized := value
		if len(normalized) > 10 && normalized[10] == 't' {
			normalized = normalized[:10] + "T" + normalized[11:]
		}
		if strings.HasSuffix(normalized, "z") {
			normalized = normalized[:len(normalized)-1] + "Z"
		}
		_, err := time.Parse(time.RFC3339Nano, normalized)
		return err == nil
	case "time":
		_, err := time.Parse("15:04:05Z07:00", value)
		return err == nil
	case "regex":
		_, err := regexp.Compile(value)
		return err == nil
	case "json-pointer":
		return validJSONPointer(value, false)
	case "json-pointer-uri-fragment":
		return strings.HasPrefix(value, "#") && validJSONPointer(strings.TrimPrefix(value, "#"), true)
	default:
		return true
	}
}

func validHostname(value string) bool {
	if len(value) == 0 || len(value) > 253 {
		return false
	}
	for _, label := range strings.Split(strings.TrimSuffix(value, "."), ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, r := range label {
			if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-') {
				return false
			}
		}
	}
	return true
}

func validJSONPointer(value string, allowEmpty bool) bool {
	if value == "" {
		return allowEmpty
	}
	if !strings.HasPrefix(value, "/") {
		return false
	}
	for _, part := range strings.Split(value[1:], "/") {
		for i := 0; i < len(part); i++ {
			if part[i] == '~' && (i+1 >= len(part) || (part[i+1] != '0' && part[i+1] != '1')) {
				return false
			}
		}
	}
	return true
}

func svProp(v *schemaValue, key string) *schemaValue {
	if v == nil || v.Object == nil {
		return nil
	}
	got, _ := v.Object.get(key)
	return got
}

func svString(v *schemaValue, key string) string {
	got := svProp(v, key)
	if got != nil && got.IsString {
		return got.String
	}
	return ""
}

func svBool(v *schemaValue, key string) *bool {
	got := svProp(v, key)
	if got != nil && got.IsBool {
		value := got.Bool
		return &value
	}
	return nil
}

func svInt(v *schemaValue, key string) *int {
	got := svProp(v, key)
	if got == nil || !got.IsNumber || !isFiniteJSONNumber(got.Number) || got.Number != math.Trunc(got.Number) {
		return nil
	}
	value := int(got.Number)
	return &value
}

func svFloat(v *schemaValue, key string) *float64 {
	got := svProp(v, key)
	if got == nil || !got.IsNumber {
		return nil
	}
	value := got.Number
	return &value
}

func valueAtPointer(root *schemaValue, pointer string) *schemaValue {
	if pointer == "" {
		return root
	}
	if !strings.HasPrefix(pointer, "/") {
		return nil
	}
	current := root
	for _, raw := range strings.Split(pointer[1:], "/") {
		key := strings.ReplaceAll(strings.ReplaceAll(raw, "~1", "/"), "~0", "~")
		if current == nil {
			return nil
		}
		if current.Object != nil {
			next, ok := current.Object.get(key)
			if !ok {
				return nil
			}
			current = next
			continue
		}
		if current.IsArray {
			index, err := strconv.Atoi(key)
			if err != nil || index < 0 || index >= len(current.Array) {
				return nil
			}
			current = current.Array[index]
			continue
		}
		return nil
	}
	return current
}

func escapeJSONPointer(key string) string {
	return strings.ReplaceAll(strings.ReplaceAll(key, "~", "~0"), "/", "~1")
}
