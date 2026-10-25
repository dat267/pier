package coding

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"testing"
)

type outputAccumulatorFixtureChunk struct {
	text *string
	data []byte
}

func (c *outputAccumulatorFixtureChunk) UnmarshalJSON(raw []byte) error {
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		c.text = &text
		return nil
	}
	var buffer struct {
		Type string `json:"type"`
		Data []byte `json:"data"`
	}
	if err := json.Unmarshal(raw, &buffer); err != nil {
		return err
	}
	c.data = buffer.Data
	return nil
}

func (c outputAccumulatorFixtureChunk) bytes() []byte {
	if c.text != nil {
		return []byte(*c.text)
	}
	return c.data
}

type outputAccumulatorFixtureResult struct {
	ContentSHA256   string `json:"contentSHA256"`
	ContentBytes    int    `json:"contentBytes"`
	Truncated       bool   `json:"truncated"`
	TruncatedBy     string `json:"truncatedBy"`
	TotalLines      int    `json:"totalLines"`
	TotalBytes      int    `json:"totalBytes"`
	OutputLines     int    `json:"outputLines"`
	OutputBytes     int    `json:"outputBytes"`
	LastLinePartial bool   `json:"lastLinePartial"`
	FullOutputPath  string `json:"fullOutputPath"`
	FileSHA256      string `json:"fileSHA256"`
}

// pi v1.4.0 core/tools/output-accumulator.ts drives the bash/PowerShell tool
// display snapshot. The corpus executes the pinned class; no shell commands run.
// Regenerate:
// node coding/testdata/output-accumulator/generate.mjs ./pi > coding/testdata/output-accumulator/golden.json
func TestOutputAccumulatorSnapshotMatchesUpstream(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	raw, err := os.ReadFile("testdata/output-accumulator/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var tests []struct {
		Name    string `json:"name"`
		Options struct {
			MaxLines *int `json:"maxLines"`
			MaxBytes *int `json:"maxBytes"`
		} `json:"options"`
		Chunks   []outputAccumulatorFixtureChunk `json:"chunks"`
		Expected outputAccumulatorFixtureResult  `json:"expected"`
	}
	if err := json.Unmarshal(raw, &tests); err != nil {
		t.Fatal(err)
	}
	if len(tests) == 0 {
		t.Fatal("empty output accumulator corpus")
	}
	for _, test := range tests {
		t.Run(test.Name, func(t *testing.T) {
			maxLines, maxBytes := 0, 0
			if test.Options.MaxLines != nil {
				maxLines = *test.Options.MaxLines
			}
			if test.Options.MaxBytes != nil {
				maxBytes = *test.Options.MaxBytes
			}
			accumulator := NewOutputAccumulator(maxLines, maxBytes, "fixture-output")
			for _, chunk := range test.Chunks {
				if err := accumulator.Append(chunk.bytes()); err != nil {
					t.Fatal(err)
				}
			}
			accumulator.Finish()
			snapshot := accumulator.Snapshot(true)
			if err := accumulator.CloseTempFile(); err != nil {
				t.Fatal(err)
			}
			got := outputAccumulatorFixtureResult{
				ContentSHA256:   fmt.Sprintf("%x", sha256.Sum256([]byte(snapshot.Content))),
				ContentBytes:    len(snapshot.Content),
				Truncated:       snapshot.Truncation.Truncated,
				TruncatedBy:     string(snapshot.Truncation.TruncatedBy),
				TotalLines:      snapshot.Truncation.TotalLines,
				TotalBytes:      snapshot.Truncation.TotalBytes,
				OutputLines:     snapshot.Truncation.OutputLines,
				OutputBytes:     snapshot.Truncation.OutputBytes,
				LastLinePartial: snapshot.Truncation.LastLinePartial,
			}
			if snapshot.FullOutputPath != "" {
				got.FullOutputPath = "present"
				full, err := os.ReadFile(snapshot.FullOutputPath)
				if err != nil {
					t.Fatal(err)
				}
				got.FileSHA256 = fmt.Sprintf("%x", sha256.Sum256(full))
			}
			if !reflect.DeepEqual(got, test.Expected) {
				t.Errorf("snapshot = %+v, want %+v", got, test.Expected)
			}
		})
	}
}
