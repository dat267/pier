package coding

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
)

// jsonUnmarshaler is encoding/json's Unmarshaler, checked on a pointer to the
// target type (custom unions such as QuietStartupSetting decode themselves).
var jsonUnmarshaler = reflect.TypeFor[json.Unmarshaler]()

// decodeSettingsTolerantWithRejects is decodeSettingsTolerant for a top-level
// settings object, returning the known member names that were present but whose
// value the typed field could not hold. A project scope uses them to mask the
// inherited global member, so a mistyped project value shadows the global one
// (and falls back to the getter default) instead of letting the global value
// show through.
func decodeSettingsTolerantWithRejects(encoded []byte, target any) []string {
	value := reflect.ValueOf(target)
	if value.Kind() != reflect.Pointer || value.IsNil() || value.Elem().Kind() != reflect.Struct {
		return nil
	}
	var members map[string]json.RawMessage
	if json.Unmarshal(encoded, &members) != nil {
		return nil
	}
	var rejected []string
	typ := value.Elem().Type()
	for index := 0; index < typ.NumField(); index++ {
		field := typ.Field(index)
		if field.PkgPath != "" {
			continue
		}
		name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
		if name == "" || name == "-" {
			continue
		}
		member, ok := members[name]
		if !ok {
			continue
		}
		if !decodeTolerantValue(member, value.Elem().Field(index)) {
			rejected = append(rejected, name)
		}
	}
	return rejected
}

// clearSettingsMember zeroes the field with the given JSON name, so a rejected
// project member falls back to its getter default after the merge.
func clearSettingsMember(settings *Settings, name string) {
	value := reflect.ValueOf(settings).Elem()
	typ := value.Type()
	for index := 0; index < typ.NumField(); index++ {
		field := typ.Field(index)
		if field.PkgPath != "" {
			continue
		}
		fieldName, _, _ := strings.Cut(field.Tag.Get("json"), ",")
		if fieldName == name {
			value.Field(index).SetZero()
			return
		}
	}
}

// decodeTolerantValue decodes one JSON value into target, reporting whether the
// target accepted anything.
func decodeTolerantValue(raw []byte, target reflect.Value) bool {
	if target.Kind() != reflect.Interface && target.CanAddr() {
		if pointer := reflect.PointerTo(target.Type()); pointer.Implements(jsonUnmarshaler) {
			fresh := reflect.New(target.Type())
			if json.Unmarshal(raw, fresh.Interface()) != nil {
				return false
			}
			target.Set(fresh.Elem())
			return true
		}
	}
	switch target.Kind() {
	case reflect.Pointer:
		element := target.Type().Elem()
		if reflect.PointerTo(element).Implements(jsonUnmarshaler) {
			fresh := reflect.New(element)
			if json.Unmarshal(raw, fresh.Interface()) != nil {
				return false
			}
			target.Set(fresh)
			return true
		}
		if element.Kind() == reflect.Struct {
			if target.IsNil() {
				target.Set(reflect.New(element))
			}
			if decodeTolerantStruct(raw, target.Elem()) {
				return true
			}
			target.SetZero()
			return false
		}
		fresh := reflect.New(element)
		if json.Unmarshal(raw, fresh.Interface()) != nil {
			return false
		}
		target.Set(fresh)
		return true
	case reflect.Struct:
		return decodeTolerantStruct(raw, target)
	case reflect.Map:
		return decodeTolerantMap(raw, target)
	case reflect.Interface:
		var value any
		if json.Unmarshal(raw, &value) != nil {
			return false
		}
		if value == nil {
			target.SetZero()
			return true
		}
		target.Set(reflect.ValueOf(value))
		return true
	default:
		fresh := reflect.New(target.Type())
		if json.Unmarshal(raw, fresh.Interface()) != nil {
			return false
		}
		target.Set(fresh.Elem())
		return true
	}
}

// decodeTolerantStruct decodes a JSON object member by member, skipping members
// whose value the field cannot hold.
func decodeTolerantStruct(raw []byte, target reflect.Value) bool {
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		// A null object is rejected like any other mismatched value.
		return false
	}
	var members map[string]json.RawMessage
	if json.Unmarshal(raw, &members) != nil {
		return false
	}
	if members == nil {
		return false
	}
	// An object counts as accepted even when it carries no known member, so a
	// present-but-empty object keeps its pointer (the reference keeps `{}`).
	acceptedObject := true
	accepted := false
	typ := target.Type()
	for index := 0; index < typ.NumField(); index++ {
		field := typ.Field(index)
		if field.PkgPath != "" {
			continue
		}
		name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
		if name == "" || name == "-" {
			continue
		}
		member, ok := members[name]
		if !ok {
			continue
		}
		if decodeTolerantValue(member, target.Field(index)) {
			accepted = true
		}
	}
	return accepted || acceptedObject
}

// decodeTolerantMap decodes a JSON object into a Go map, skipping entries the
// element type cannot hold.
func decodeTolerantMap(raw []byte, target reflect.Value) bool {
	var members map[string]json.RawMessage
	if json.Unmarshal(raw, &members) != nil {
		return false
	}
	if len(members) == 0 {
		target.Set(reflect.MakeMap(target.Type()))
		return true
	}
	result := reflect.MakeMapWithSize(target.Type(), len(members))
	accepted := false
	for key, member := range members {
		value := reflect.New(target.Type().Elem()).Elem()
		if decodeTolerantValue(member, value) {
			result.SetMapIndex(reflect.ValueOf(key).Convert(target.Type().Key()), value)
			accepted = true
		}
	}
	if !accepted {
		return false
	}
	target.Set(result)
	return true
}
