package durable

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Port of the durable edit and bash tools.

func TestEditTool(t *testing.T) {
	env, dir := newToolTestEnv(t)
	ctx := context.Background()
	path := filepath.Join(dir, "a.txt")
	if err := env.WriteFile(path, []byte("one\ntwo\nthree\n"), ctx); err != nil {
		t.Fatal(err)
	}
	api := &fakeToolApi{env: env}
	result, err := CreateEditTool().Execute(JsonObject{
		"path": "a.txt",
		"edits": []any{
			map[string]any{"oldText": "two", "newText": "TWO"},
		},
	}, api, ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Content) != 1 {
		t.Fatalf("content = %+v", result.Content)
	}
	if result.Details == nil {
		t.Fatalf("details = %+v", result.Details)
	}
	content, err := os.ReadFile(path)
	if err != nil || string(content) != "one\nTWO\nthree\n" {
		t.Fatalf("content = %q, %v", content, err)
	}
	// A missing oldText fails.
	if _, err := CreateEditTool().Execute(JsonObject{
		"path":  "a.txt",
		"edits": []any{map[string]any{"oldText": "absent", "newText": "x"}},
	}, api, ctx); err == nil {
		t.Fatal("a missing oldText must fail")
	}
	// An empty edits array fails validation.
	if _, err := CreateEditTool().Execute(JsonObject{"path": "a.txt", "edits": []any{}}, api, ctx); err == nil {
		t.Fatal("an empty edits array must fail")
	}
}

func TestPrepareEditArguments(t *testing.T) {
	// A JSON-string edits array is parsed.
	repaired, err := PrepareEditArguments(JsonObject{
		"path": "a", "edits": `[{"oldText":"x","newText":"y"}]`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if list, ok := repaired.(map[string]any)["edits"].([]any); !ok || len(list) != 1 {
		t.Fatalf("repaired = %+v", repaired)
	}
	// A single edit object becomes an array.
	repaired, err = PrepareEditArguments(JsonObject{
		"path": "a", "edits": map[string]any{"oldText": "x", "newText": "y"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if list, ok := repaired.(map[string]any)["edits"].([]any); !ok || len(list) != 1 {
		t.Fatalf("repaired = %+v", repaired)
	}
	// A legacy top-level pair is folded into edits.
	repaired, err = PrepareEditArguments(JsonObject{"path": "a", "oldText": "x", "newText": "y"})
	if err != nil {
		t.Fatal(err)
	}
	object := repaired.(map[string]any)
	if _, present := object["oldText"]; present {
		t.Fatalf("repaired = %+v", repaired)
	}
	if list, ok := object["edits"].([]any); !ok || len(list) != 1 {
		t.Fatalf("repaired = %+v", repaired)
	}
}

func TestBashTool(t *testing.T) {
	env, _ := newToolTestEnv(t)
	ctx := context.Background()
	api := &fakeToolApi{env: env}
	if _, err := CreateBashTool(nil).Execute(JsonObject{"command": "echo hi"}, api, ctx); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(api.output.String(), "hi") {
		t.Fatalf("output = %q", api.output.String())
	}
	// An invalid timeout is rejected.
	if _, err := CreateBashTool(nil).Execute(JsonObject{"command": "echo", "timeout": float64(0)}, api, ctx); err == nil {
		t.Fatal("a zero timeout must fail")
	}
	// A nonzero exit is an error.
	if _, err := CreateBashTool(nil).Execute(JsonObject{"command": "exit 3"}, api, ctx); err == nil ||
		!strings.Contains(err.Error(), "Command exited with code 3") {
		t.Fatalf("err = %v", err)
	}
}
