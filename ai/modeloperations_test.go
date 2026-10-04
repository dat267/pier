package ai

import "testing"

// TestModelTypeHelpers covers getModelType/isModelType: an absent type is chat.
func TestModelTypeHelpers(t *testing.T) {
	if got := GetModelType(&Model{ID: "m"}); got != "chat" {
		t.Fatalf("absent type = %q", got)
	}
	if got := GetModelType(&Model{ID: "m", Type: "image"}); got != "image" {
		t.Fatalf("explicit type = %q", got)
	}
	if !IsModelType(&Model{ID: "m"}, "chat") {
		t.Fatal("a model without a type must be chat")
	}
	if IsModelType(&Model{ID: "m", Type: "image"}, "chat") {
		t.Fatal("an image model is not chat")
	}
	if !IsModelType(&Model{ID: "m", Type: "image"}, "image") {
		t.Fatal("an image model is image")
	}
}

// TestBuiltinModelCarriesType pins that the catalog's type survives decoding.
func TestBuiltinModelCarriesType(t *testing.T) {
	model := GetBuiltinModel("anthropic", "claude-sonnet-4-5")
	if model == nil {
		t.Skip("catalog model missing")
	}
	if model.Type != "chat" {
		t.Fatalf("type = %q", model.Type)
	}
}
