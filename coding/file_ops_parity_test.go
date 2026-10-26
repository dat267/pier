package coding

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"

	"github.com/dat267/pier/ai"
)

// pi v1.4.0 (4a080252), core/compaction/utils.ts file-operation tracking.
// Regenerate:
//
//	node --experimental-strip-types coding/testdata/file-ops/generate.mjs ./pi > coding/testdata/file-ops/golden.json
func TestFileOperationsMatchUpstream(t *testing.T) {
	raw, err := os.ReadFile("testdata/file-ops/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Name          string            `json:"name"`
		Messages      []json.RawMessage `json:"messages"`
		ReadFiles     []string          `json:"readFiles"`
		ModifiedFiles []string          `json:"modifiedFiles"`
		Formatted     string            `json:"formatted"`
	}
	if err := json.Unmarshal(raw, &fixtures); err != nil {
		t.Fatal(err)
	}
	if len(fixtures) == 0 {
		t.Fatal("empty file-operations corpus")
	}
	for _, fixture := range fixtures {
		t.Run(fixture.Name, func(t *testing.T) {
			ops := CreateFileOps()
			for _, encoded := range fixture.Messages {
				message, err := ai.UnmarshalMessage(encoded)
				if err != nil {
					t.Fatalf("decode message: %v", err)
				}
				ExtractFileOpsFromMessage(message, ops)
			}
			readFiles, modifiedFiles := ComputeFileLists(ops)
			if !reflect.DeepEqual(readFiles, fixture.ReadFiles) {
				t.Errorf("read files = %v, want %v", readFiles, fixture.ReadFiles)
			}
			if !reflect.DeepEqual(modifiedFiles, fixture.ModifiedFiles) {
				t.Errorf("modified files = %v, want %v", modifiedFiles, fixture.ModifiedFiles)
			}
			if got := FormatFileOperations(readFiles, modifiedFiles); got != fixture.Formatted {
				t.Errorf("formatted = %q, want %q", got, fixture.Formatted)
			}
		})
	}
}
