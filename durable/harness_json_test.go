package durable

import (
	"encoding/json"
	"testing"
)

// Port of harness/json.ts.

func decodeJSON(t *testing.T, text string) map[string]any {
	t.Helper()
	var value map[string]any
	if err := json.Unmarshal([]byte(text), &value); err != nil {
		t.Fatal(err)
	}
	return value
}

func TestAssignJSONMergesObjects(t *testing.T) {
	target := decodeJSON(t, `{"child":{"x":1,"y":2}}`)
	// Assigning an object recurses: absent keys are removed, present ones are
	// assigned leaf by leaf.
	AssignJSON(target, "child", map[string]any{"x": float64(9)})
	child := target["child"].(map[string]any)
	if child["x"] != float64(9) {
		t.Fatalf("child = %+v", child)
	}
	if _, present := child["y"]; present {
		t.Fatalf("child = %+v", child)
	}
	// A leaf assignment replaces just that key.
	AssignJSON(child, "x", float64(10))
	if child["x"] != float64(10) {
		t.Fatalf("child = %+v", child)
	}
}

func TestAssignJSONArrays(t *testing.T) {
	target := decodeJSON(t, `{"list":[1,2]}`)
	AssignJSON(target, "list", []any{float64(3), float64(4), float64(5)})
	list := target["list"].([]any)
	if len(list) != 3 || list[0] != float64(3) || list[2] != float64(5) {
		t.Fatalf("list = %v", list)
	}
	// A shrinking array replaces wholesale.
	AssignJSON(target, "list", []any{float64(9)})
	list = target["list"].([]any)
	if len(list) != 1 || list[0] != float64(9) {
		t.Fatalf("list = %v", list)
	}
}

func TestAssignJSONPrimitives(t *testing.T) {
	target := decodeJSON(t, `{"a":1,"b":"x"}`)
	AssignJSON(target, "a", float64(2))
	AssignJSON(target, "b", "x")
	AssignJSON(target, "c", true)
	if target["a"] != float64(2) || target["b"] != "x" || target["c"] != true {
		t.Fatalf("target = %+v", target)
	}
}
