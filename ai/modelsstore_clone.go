package ai

import "reflect"

// Clone returns an independent model-store snapshot, matching structuredClone
// in pi v1.4.0 (4a080252), src/models-store.ts. Store writes still validate and
// normalize incoming descriptors through JSON; reads clone the validated data
// without repeating that serialization work.
func (entry *ModelsStoreEntry) Clone() *ModelsStoreEntry {
	if entry == nil {
		return nil
	}
	cloned := cloneModelStoreValue(reflect.ValueOf(entry)).Interface().(*ModelsStoreEntry)
	// Keep D271's array shape when reading a legacy null catalog.
	if cloned.Models == nil {
		cloned.Models = []*Model{}
	}
	return cloned
}

// Model descriptors contain only records, maps, slices, pointers and scalar
// values. Copy their exported fields recursively rather than hand-maintaining
// copies of every compatibility flag. Model's private JSON source and baseline
// are immutable provenance, safely shared by independent clones;
// MarshalJSON reads them and UnmarshalJSON replaces them, never mutating them.
func cloneModelStoreValue(value reflect.Value) reflect.Value {
	switch value.Kind() {
	case reflect.Pointer:
		if value.IsNil() {
			return value
		}
		cloned := reflect.New(value.Type().Elem())
		cloned.Elem().Set(cloneModelStoreValue(value.Elem()))
		return cloned
	case reflect.Struct:
		cloned := reflect.New(value.Type()).Elem()
		cloned.Set(value)
		for i := 0; i < value.NumField(); i++ {
			if field := cloned.Field(i); field.CanSet() {
				field.Set(cloneModelStoreValue(value.Field(i)))
			}
		}
		return cloned
	case reflect.Map:
		if value.IsNil() {
			return value
		}
		cloned := reflect.MakeMapWithSize(value.Type(), value.Len())
		for entries := value.MapRange(); entries.Next(); {
			// All descriptor map keys are immutable string types.
			cloned.SetMapIndex(entries.Key(), cloneModelStoreValue(entries.Value()))
		}
		return cloned
	case reflect.Slice:
		if value.IsNil() {
			return value
		}
		cloned := reflect.MakeSlice(value.Type(), value.Len(), value.Len())
		if kind := value.Type().Elem().Kind(); kind == reflect.Uint8 || kind == reflect.String {
			reflect.Copy(cloned, value)
		} else {
			for i := 0; i < value.Len(); i++ {
				cloned.Index(i).Set(cloneModelStoreValue(value.Index(i)))
			}
		}
		return cloned
	default:
		return value
	}
}
