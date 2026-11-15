package ai

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// The store read path clones a validated entry instead of serializing it again
// (D271 keeps the null-catalog shape). This benchmark keeps that claim measured:
// a read that fell back to JSON would pay the marshal and unmarshal below.
func BenchmarkModelsStoreClone(b *testing.B) {
	var models []string
	for index := range 200 {
		models = append(models, fmt.Sprintf(`{"id":"model-%d","name":"Model %d","api":"openai-completions","provider":"test",`+
			`"input":["text","image"],"cost":{"input":1,"output":2,"cacheRead":0,"cacheWrite":0},`+
			`"contextWindow":200000,"maxTokens":8192,"headers":{"x-test":"value"},`+
			`"compat":{"supportsStore":true},"samplingParams":{"temperature":0.2}}`, index, index))
	}
	var entry ModelsStoreEntry
	if err := json.Unmarshal([]byte(`{"models":[`+strings.Join(models, ",")+`],"lastModified":1}`), &entry); err != nil {
		b.Fatal(err)
	}

	b.Run("clone", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if cloned := entry.Clone(); len(cloned.Models) != len(entry.Models) {
				b.Fatal("clone lost models")
			}
		}
	})
	b.Run("json-round-trip", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			encoded, err := MarshalJSON(&entry)
			if err != nil {
				b.Fatal(err)
			}
			var decoded ModelsStoreEntry
			if err := json.Unmarshal(encoded, &decoded); err != nil {
				b.Fatal(err)
			}
		}
	})
}
