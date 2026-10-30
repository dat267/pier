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
// settings object, returning the known member names that must shadow inherited
// values. This includes values the typed field cannot hold and explicit nulls,
// whose presence must be retained separately from the field's zero value. A
// project scope uses them to mask the global member, matching upstream's
// undefined-versus-null merge behavior.
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
		path := []string{name}
		if bytes.Equal(bytes.TrimSpace(member), []byte("null")) {
			rejected = append(rejected, encodeSettingsPath(path))
			continue
		}
		if !decodeTolerantValue(member, value.Elem().Field(index)) {
			rejected = append(rejected, encodeSettingsPath(path))
			continue
		}
		rejected = append(rejected, explicitNullSettingsPaths(member, field.Type, path)...)
	}
	return rejected
}

// explicitNullSettingsPaths finds nested JSON nulls whose presence cannot be
// represented by Settings' pointer-zero values. It follows structs and maps,
// matching the object-recursive settings merge without treating array entries
// as object overrides.
func explicitNullSettingsPaths(raw []byte, typ reflect.Type, path []string) []string {
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	if reflect.PointerTo(typ).Implements(jsonUnmarshaler) {
		return nil
	}
	trimmed := bytes.TrimSpace(raw)
	if bytes.Equal(trimmed, []byte("null")) {
		return []string{encodeSettingsPath(path)}
	}
	if typ.Kind() == reflect.Interface {
		if len(trimmed) == 0 || trimmed[0] != '{' {
			return nil
		}
		typ = reflect.TypeFor[map[string]any]()
	}
	var members map[string]json.RawMessage
	if json.Unmarshal(trimmed, &members) != nil || members == nil {
		return nil
	}
	var out []string
	switch typ.Kind() {
	case reflect.Struct:
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
			out = append(out, explicitNullSettingsPaths(member, field.Type, appendPath(path, name))...)
		}
	case reflect.Map:
		if typ.Key().Kind() != reflect.String {
			return nil
		}
		for name, member := range members {
			out = append(out, explicitNullSettingsPaths(member, typ.Elem(), appendPath(path, name))...)
		}
	}
	return out
}

func appendPath(path []string, segment string) []string {
	out := make([]string, len(path)+1)
	copy(out, path)
	out[len(path)] = segment
	return out
}

func encodeSettingsPath(path []string) string {
	var encoded strings.Builder
	for _, segment := range path {
		encoded.WriteByte('/')
		segment = strings.ReplaceAll(segment, "~", "~0")
		segment = strings.ReplaceAll(segment, "/", "~1")
		encoded.WriteString(segment)
	}
	return encoded.String()
}

func decodeSettingsPath(path string) []string {
	if !strings.HasPrefix(path, "/") {
		return []string{path}
	}
	parts := strings.Split(path[1:], "/")
	for index, part := range parts {
		part = strings.ReplaceAll(part, "~1", "/")
		parts[index] = strings.ReplaceAll(part, "~0", "~")
	}
	return parts
}

// clearSettingsMember zeroes a rejected field path, so an explicit project
// null or unsupported value falls back to the setting's default after merge.
func clearSettingsMember(settings *Settings, path string) {
	clearSettingsPath(reflect.ValueOf(settings).Elem(), decodeSettingsPath(path))
}

func clearSettingsPath(value reflect.Value, path []string) {
	if len(path) == 0 {
		if value.CanSet() {
			value.SetZero()
		}
		return
	}
	for value.Kind() == reflect.Pointer || value.Kind() == reflect.Interface {
		if value.IsNil() {
			return
		}
		value = value.Elem()
	}
	switch value.Kind() {
	case reflect.Struct:
		typ := value.Type()
		for index := 0; index < typ.NumField(); index++ {
			field := typ.Field(index)
			if field.PkgPath != "" {
				continue
			}
			name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
			if name == path[0] {
				clearSettingsPath(value.Field(index), path[1:])
				return
			}
		}
	case reflect.Map:
		if value.Type().Key().Kind() != reflect.String {
			return
		}
		key := reflect.ValueOf(path[0]).Convert(value.Type().Key())
		entry := value.MapIndex(key)
		if !entry.IsValid() {
			return
		}
		if len(path) == 1 {
			value.SetMapIndex(key, reflect.Zero(value.Type().Elem()))
			return
		}
		copy := reflect.New(entry.Type()).Elem()
		copy.Set(entry)
		clearSettingsPath(copy, path[1:])
		value.SetMapIndex(key, copy)
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
