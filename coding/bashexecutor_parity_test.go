package coding

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
)

type bashExecutorFixtureChunk struct {
	Text   string `json:"text"`
	Repeat *int   `json:"repeat"`
}

type bashExecutorFixtureOperations struct{ chunks []bashExecutorFixtureChunk }

func (operations bashExecutorFixtureOperations) Exec(_ context.Context, _, _ string, options BashExecOptions) (*int, error) {
	for _, chunk := range operations.chunks {
		count := 1
		if chunk.Repeat != nil {
			count = *chunk.Repeat
		}
		options.OnData([]byte(strings.Repeat(chunk.Text, count)))
	}
	code := 0
	return &code, nil
}

type bashExecutorFixtureResult struct {
	OutputSHA256 string `json:"outputSHA256"`
	OutputBytes  int    `json:"outputBytes"`
	Truncated    bool   `json:"truncated"`
	Cancelled    bool   `json:"cancelled"`
	ExitCode     *int   `json:"exitCode"`
	HasFile      bool   `json:"hasFile"`
	FileSHA256   string `json:"fileSHA256"`
}

// pi v1.4.0 core/bash-executor.ts sizes its rolling display chunks with
// JS string.length (UTF-16), not UTF-8 bytes. The real pinned function generates
// output and full-file hashes; no shell commands are executed. Regenerate:
// node coding/testdata/bash-executor/generate.mjs ./pi > coding/testdata/bash-executor/golden.json
func TestBashExecutorRollingOutputMatchesUpstream(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	raw, err := os.ReadFile("testdata/bash-executor/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var tests []struct {
		Name     string                     `json:"name"`
		Chunks   []bashExecutorFixtureChunk `json:"chunks"`
		Expected bashExecutorFixtureResult  `json:"expected"`
	}
	if err := json.Unmarshal(raw, &tests); err != nil {
		t.Fatal(err)
	}
	if len(tests) == 0 {
		t.Fatal("empty bash executor corpus")
	}
	for _, test := range tests {
		t.Run(test.Name, func(t *testing.T) {
			result, err := ExecuteBashWithOperations(context.Background(), "fixture", t.TempDir(), bashExecutorFixtureOperations{test.Chunks}, nil)
			if err != nil {
				t.Fatal(err)
			}
			got := bashExecutorFixtureResult{
				OutputSHA256: fmt.Sprintf("%x", sha256.Sum256([]byte(result.Output))),
				OutputBytes:  len(result.Output), Truncated: result.Truncated, Cancelled: result.Cancelled,
				ExitCode: result.ExitCode, HasFile: result.FullOutputPath != "",
			}
			if got.HasFile {
				full, err := os.ReadFile(result.FullOutputPath)
				if err != nil {
					t.Fatal(err)
				}
				got.FileSHA256 = fmt.Sprintf("%x", sha256.Sum256(full))
			}
			if !reflect.DeepEqual(got, test.Expected) {
				t.Errorf("result = %+v, want %+v", got, test.Expected)
			}
		})
	}
}
