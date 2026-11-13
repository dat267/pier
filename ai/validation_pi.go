package ai

import (
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"net"
	"net/mail"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
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

	ref        string
	dynamicRef string

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
	if schema.dynamicRef != "" {
		if target, ok := schema.resolveRef(schema.dynamicRef); ok && target != nil {
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
			s.items = make([]*piSchema, 0, len(items.Array))
			for i := range items.Array {
				s.items = append(s.items, s.doc.node(s.pointer+"/items/"+strconv.Itoa(i)))
			}
		} else {
			s.itemsOne = s.doc.node(s.pointer + "/items")
		}
	}
	if prefix := svProp(v, "prefixItems"); prefix != nil && prefix.IsArray {
		s.prefixItems = make([]*piSchema, 0, len(prefix.Array))
		for i := range prefix.Array {
			s.prefixItems = append(s.prefixItems, s.doc.node(s.pointer+"/prefixItems/"+strconv.Itoa(i)))
		}
	}
	if add := svProp(v, "additionalItems"); add != nil {
		s.additionalItems = s.doc.node(s.pointer + "/additionalItems")
	}
	if add := svProp(v, "additionalProperties"); add != nil {
		if add.IsBool && !add.Bool {
			s.additionalFalse = true
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
		s.enum = append(s.enum, enum.Array...)
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
	s.dynamicRef = svString(v, "$dynamicRef")
	if u := svProp(v, "unevaluatedProperties"); u != nil {
		if u.IsBool && !u.Bool {
			s.unevaluatedFalse = true
		} else {
			s.unevaluatedProperties = s.doc.node(s.pointer + "/unevaluatedProperties")
		}
	}
	if u := svProp(v, "unevaluatedItems"); u != nil {
		if u.IsBool && !u.Bool {
			s.unevaluatedItemsFalse = true
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
	return s.resolveRef(s.ref)
}

func (s *piSchema) resolveRef(ref string) (*piSchema, bool) {
	if ref == "" {
		return nil, false
	}
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
	if s.dynamicRef != "" {
		target, ok := s.resolveRef(s.dynamicRef)
		if !ok || target == nil || !target.checkActive(value, active) {
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
		annotations := s.annotations(value)
		if !annotations.allProps {
			for _, key := range value.Object.keys {
				if annotations.props[key] {
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
	if value.IsArray && (s.unevaluatedItemsFalse || s.unevaluatedItems != nil) {
		annotations := s.annotations(value)
		if !annotations.allItems {
			for index := annotations.items; index < len(value.Array); index++ {
				if annotations.indexes[index] {
					continue
				}
				if s.unevaluatedItemsFalse {
					return false
				}
				if !s.unevaluatedItems.checkActive(value.Array[index], active) {
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

type piAnnotations struct {
	props    map[string]bool
	allProps bool
	items    int
	allItems bool
	indexes  map[int]bool
}

func (a *piAnnotations) addProp(key string) {
	if a.props == nil {
		a.props = map[string]bool{}
	}
	a.props[key] = true
}

func (a *piAnnotations) addIndex(index int) {
	if a.indexes == nil {
		a.indexes = map[int]bool{}
	}
	a.indexes[index] = true
}

func (s *piSchema) annotations(value *schemaValue) piAnnotations {
	out := piAnnotations{}
	s.addAnnotations(value, &out, false)
	return out
}

func (s *piSchema) addAnnotations(value *schemaValue, out *piAnnotations, forParent bool) {
	if s == nil || s.false || value == nil {
		return
	}
	if s.ref != "" {
		if target, ok := s.resolve(); ok {
			target.addAnnotations(value, out, true)
		}
	}
	if s.dynamicRef != "" {
		if target, ok := s.resolveRef(s.dynamicRef); ok {
			target.addAnnotations(value, out, true)
		}
	}
	if value.Object != nil {
		for _, key := range s.propertyOrder {
			if _, ok := value.Object.get(key); ok {
				out.addProp(key)
			}
		}
		for _, key := range value.Object.keys {
			for _, pattern := range s.patternOrder {
				if re := s.patternRes[pattern]; re != nil && re.MatchString(key) {
					out.addProp(key)
				}
			}
		}
		if s.additionalProps != nil || s.additionalFalse {
			for _, key := range value.Object.keys {
				_, declared := s.properties[key]
				matched := declared
				for _, pattern := range s.patternOrder {
					if re := s.patternRes[pattern]; re != nil && re.MatchString(key) {
						matched = true
					}
				}
				if !matched {
					out.addProp(key)
				}
			}
		}
	}
	if value.IsArray {
		prefix := s.items
		if s.prefixItems != nil {
			prefix = s.prefixItems
		}
		applied := len(value.Array)
		if applied > len(prefix) {
			applied = len(prefix)
		}
		if applied > out.items {
			out.items = applied
		}
		rest := s.itemsOne
		if s.items != nil {
			rest = s.additionalItems
		}
		if rest != nil || s.itemsOne != nil {
			out.allItems = true
		}
		if s.contains != nil {
			for index, item := range value.Array {
				if s.contains.check(item) {
					out.addIndex(index)
				}
			}
		}
	}
	for _, sub := range s.allOf {
		sub.addAnnotations(value, out, true)
	}
	for _, branches := range [][]*piSchema{s.anyOf, s.oneOf} {
		for _, branch := range branches {
			if branch.check(value) {
				branch.addAnnotations(value, out, true)
			}
		}
	}
	if s.ifS != nil {
		if s.ifS.check(value) {
			s.ifS.addAnnotations(value, out, true)
			if s.thenS != nil {
				s.thenS.addAnnotations(value, out, true)
			}
		} else if s.elseS != nil {
			s.elseS.addAnnotations(value, out, true)
		}
	}
	if value.Object != nil {
		for key, dep := range s.dependentSchemas {
			if _, ok := value.Object.get(key); ok {
				dep.addAnnotations(value, out, true)
			}
		}
		for key, dep := range s.dependencies {
			if _, ok := value.Object.get(key); ok {
				dep.addAnnotations(value, out, true)
			}
		}
	}
	// In a parent scope, a passing unevaluated keyword evaluates everything that
	// remains. The schema's own check must see the annotations before that mark,
	// so only parent calls set it.
	if forParent {
		if s.unevaluatedFalse || s.unevaluatedProperties != nil {
			out.allProps = true
		}
		if s.unevaluatedItemsFalse || s.unevaluatedItems != nil {
			out.allItems = true
		}
	}
}

func (s *piSchema) hasObjectKeywords() bool {
	return len(s.properties) > 0 || len(s.required) > 0 || len(s.patternProps) > 0 || s.additionalProps != nil || s.additionalFalse || s.propertyNames != nil || s.minProperties != nil || s.maxProperties != nil || len(s.dependentReq) > 0 || len(s.dependentSchemas) > 0 || len(s.dependencies) > 0 || len(s.dependencyLists) > 0
}

func (s *piSchema) hasArrayKeywords() bool {
	return s.itemsOne != nil || s.items != nil || s.prefixItems != nil || s.additionalItems != nil || s.minItems != nil || s.maxItems != nil || s.uniqueItems || s.contains != nil
}

func (s *piSchema) itemAt(index int) *piSchema {
	if index < len(s.prefixItems) {
		return s.prefixItems[index]
	}
	if index < len(s.items) {
		return s.items[index]
	}
	if s.prefixItems != nil {
		return s.itemsOne
	}
	if s.items != nil {
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
	v, d := big.NewInt(vm), big.NewInt(dm)
	if ve >= de {
		v.Mul(v, intPow10(ve-de))
	} else {
		d.Mul(d, intPow10(de-ve))
	}
	return new(big.Int).Rem(v, d).Sign() == 0
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

func intPow10(n int) *big.Int {
	return new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(n)), nil)
}

var (
	piDurationRE        = regexp.MustCompile(`^P((\d+Y(\d+M(\d+D)?)?|\d+M(\d+D)?|\d+D)(T(\d+H(\d+M(\d+S)?)?|\d+M(\d+S)?|\d+S))?|T(\d+H(\d+M(\d+S)?)?|\d+M(\d+S)?|\d+S)|\d+W)$`)
	piRelativePointerRE = regexp.MustCompile(`^(?:0|[1-9][0-9]*)(?:#|(?:/(?:[^~/]|~0|~1)*)*)$`)
	piUUIDRE            = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
	piTimeRE            = regexp.MustCompile(`^(\d\d):(\d\d):(\d\d)(?:\.\d+)?(?:([Zz])|([+-])(\d\d):(\d\d))?$`)
	piDateTimeSplitRE   = regexp.MustCompile(`(?i)t`)
)

func piFormatValid(name, value string) bool {
	switch name {
	case "email":
		addr, err := mail.ParseAddress(value)
		return err == nil && addr.Address == value && strings.Contains(value, "@")
	case "idn-email":
		at := strings.LastIndexByte(value, '@')
		if at <= 0 || at == len(value)-1 || strings.ContainsAny(value, " <>") {
			return false
		}
		return validIDNLabel(value[at+1:])
	case "uri":
		return validURIText(value, false) && absoluteURI(value)
	case "url":
		return validURIText(value, false) && absoluteURL(value)
	case "uri-reference":
		return validURIText(value, false)
	case "iri":
		return validURIText(value, true) && absoluteURI(value)
	case "iri-reference":
		return validURIText(value, true)
	case "hostname":
		return validHostname(value)
	case "idn-hostname":
		return validIDNLabel(value)
	case "ipv4":
		ip := net.ParseIP(value)
		return ip != nil && ip.To4() != nil && !strings.Contains(value, ":")
	case "ipv6":
		ip := net.ParseIP(value)
		return ip != nil && ip.To4() == nil && strings.Contains(value, ":")
	case "uuid":
		return piUUIDRE.MatchString(value)
	case "date":
		return validDate(value)
	case "date-time":
		parts := piDateTimeSplitRE.Split(value, 2)
		return len(parts) == 2 && validDate(parts[0]) && validTime(parts[1])
	case "time":
		return validTime(value)
	case "duration":
		return piDurationRE.MatchString(value)
	case "regex":
		_, err := regexp.Compile(value)
		return err == nil
	case "json-pointer":
		return validJSONPointer(value, true)
	case "json-pointer-uri-fragment":
		return strings.HasPrefix(value, "#") && validJSONPointer(strings.TrimPrefix(value, "#"), true)
	case "relative-json-pointer":
		return piRelativePointerRE.MatchString(value)
	case "uri-template":
		return validURITemplate(value)
	default:
		return true
	}
}

func validURIText(value string, allowUnicode bool) bool {
	for i := 0; i < len(value); i++ {
		c := value[i]
		if c <= 0x20 || c == 0x7f || c == '<' || c == '>' || c == '^' || c == '`' || c == '{' || c == '|' || c == '}' || c == '\\' {
			return false
		}
		if c == '%' {
			if i+2 >= len(value) || !isHex(value[i+1]) || !isHex(value[i+2]) {
				return false
			}
			i += 2
			continue
		}
		if c >= 0x80 && !allowUnicode {
			return false
		}
	}
	if allowUnicode && !utf8.ValidString(value) {
		return false
	}
	return true
}

func absoluteURI(value string) bool {
	u, err := url.Parse(value)
	return err == nil && u.Scheme != ""
}

func absoluteURL(value string) bool {
	u, err := url.Parse(value)
	return err == nil && u.Scheme != "" && u.Host != ""
}

func validIDNLabel(value string) bool {
	if value == "" || strings.HasSuffix(value, ".") {
		value = strings.TrimSuffix(value, ".")
	}
	if value == "" || len(value) > 253 {
		return false
	}
	for _, label := range strings.Split(value, ".") {
		if label == "" || utf8.RuneCountInString(label) > 63 {
			return false
		}
		runes := []rune(label)
		if runes[0] == '-' || runes[len(runes)-1] == '-' {
			return false
		}
		for _, r := range runes {
			if r != '-' && !unicode.IsLetter(r) && !unicode.IsNumber(r) {
				return false
			}
		}
	}
	return true
}

func validDate(value string) bool {
	parts := strings.Split(value, "-")
	if len(parts) != 3 || len(parts[0]) != 4 || len(parts[1]) != 2 || len(parts[2]) != 2 {
		return false
	}
	year, e1 := strconv.Atoi(parts[0])
	month, e2 := strconv.Atoi(parts[1])
	day, e3 := strconv.Atoi(parts[2])
	if e1 != nil || e2 != nil || e3 != nil || month < 1 || month > 12 || day < 1 {
		return false
	}
	days := []int{0, 31, 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31}
	if month == 2 && (year%4 == 0 && (year%100 != 0 || year%400 == 0)) {
		return day <= 29
	}
	return day <= days[month]
}

func validTime(value string) bool {
	m := piTimeRE.FindStringSubmatch(value)
	if m == nil || (m[4] == "" && m[5] == "") {
		return false
	}
	hour, _ := strconv.Atoi(m[1])
	minute, _ := strconv.Atoi(m[2])
	second, _ := strconv.Atoi(m[3])
	if hour > 23 || minute > 59 || second > 60 {
		return false
	}
	if m[5] != "" {
		tzHour, _ := strconv.Atoi(m[6])
		tzMinute, _ := strconv.Atoi(m[7])
		if tzHour > 23 || tzMinute > 59 {
			return false
		}
	}
	if second < 60 {
		return true
	}
	sign := 1
	if m[5] == "-" {
		sign = -1
	}
	tzHour, _ := strconv.Atoi(m[6])
	tzMinute, _ := strconv.Atoi(m[7])
	total := hour*60 + minute - sign*(tzHour*60+tzMinute)
	return ((total%1440)+1440)%1440 == 1439
}

func validURITemplate(value string) bool {
	for i := 0; i < len(value); i++ {
		if value[i] <= 0x20 || value[i] == 0x7f || value[i] == '"' || value[i] == '<' || value[i] == '>' || value[i] == '\\' || value[i] == '^' || value[i] == '`' || value[i] == '|' {
			return false
		}
	}
	re := regexp.MustCompile(`^(?:(?:[^\x00-\x20"<>%\\^` + "`" + `{|}\x7f]|%[0-9a-f]{2})|\{[+#./;?&=,!@|]?(?:[a-z0-9_]|%[0-9a-f]{2})+(?:\.(?:[a-z0-9_]|%[0-9a-f]{2})+)*(?::[1-9]\d{0,3}|\*)?(?:,(?:[a-z0-9_]|%[0-9a-f]{2})+(?:\.(?:[a-z0-9_]|%[0-9a-f]{2})+)*(?::[1-9]\d{0,3}|\*)?)*\})*$`)
	return re.MatchString(value)
}

func isHex(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F'
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
			if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '-' {
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
