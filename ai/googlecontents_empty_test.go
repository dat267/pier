package ai

import (
	"encoding/json"
	"strings"
	"testing"
)

// Reference: pi v1.5.0 (f9a26365) packages/ai/src/api/google-shared.ts
// convertMessages builds `const contents: Content[] = []`, so an empty
// transcript yields an empty contents array. Pier's nil slice marshals as
// `null` instead, and google_payload writes that to the wire, so an empty
// context would send `"contents": null` where the reference sends `[]`.
func TestConvertGoogleMessagesEmptyIsAnArray(t *testing.T) {
	got, err := ConvertGoogleMessages(&Model{ID: "gemini-2.5-pro", Provider: "google"}, TranscriptContext{})
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("contents is nil, want an empty non-nil slice")
	}
	encoded, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if string(encoded) != "[]" {
		t.Fatalf("empty contents = %s, want []", encoded)
	}
	if strings.Contains(string(encoded), "null") {
		t.Fatalf("contents contains null: %s", encoded)
	}
}
