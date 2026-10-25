package coding

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
)

type truncateFixtureResult struct {
	Content               string  `json:"content"`
	Truncated             bool    `json:"truncated"`
	TruncatedBy           *string `json:"truncatedBy"`
	TotalLines            int     `json:"totalLines"`
	TotalBytes            int     `json:"totalBytes"`
	OutputLines           int     `json:"outputLines"`
	OutputBytes           int     `json:"outputBytes"`
	LastLinePartial       bool    `json:"lastLinePartial"`
	FirstLineExceedsLimit bool    `json:"firstLineExceedsLimit"`
	MaxLines              int     `json:"maxLines"`
	MaxBytes              int     `json:"maxBytes"`
}

type truncateFixtureLineResult struct {
	Text          string `json:"text"`
	WasTruncated  bool   `json:"wasTruncated"`
	TextSHA256    string `json:"-"`
	ExpectedBytes int    `json:"-"`
}

// pi v1.4.0 core/tools/truncate.ts drives every tool's output limits. The
// corpus executes the pinned functions over head, tail, line, middle and size
// cases. Regenerate:
// node coding/testdata/truncate/generate.mjs ./pi > coding/testdata/truncate/golden.json
func TestTruncationMatchesUpstream(t *testing.T) {
	raw, err := os.ReadFile("testdata/truncate/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name    string `json:"name"`
		Kind    string `json:"kind"`
		Content string `json:"content"`
		Options struct {
			MaxLines *int `json:"maxLines"`
			MaxBytes *int `json:"maxBytes"`
		} `json:"options"`
		Line     string          `json:"line"`
		MaxChars *int            `json:"maxChars"`
		Bytes    int             `json:"bytes"`
		MaxBytes *int            `json:"maxBytes"`
		Expected json.RawMessage `json:"expected"`
	}
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) == 0 {
		t.Fatal("empty truncate corpus")
	}
	for _, test := range cases {
		t.Run(test.Name, func(t *testing.T) {
			switch {
			case test.Kind == "size":
				var want string
				if err := json.Unmarshal(test.Expected, &want); err != nil {
					t.Fatal(err)
				}
				if got := FormatSize(int64(test.Bytes)); got != want {
					t.Errorf("FormatSize(%d) = %q, want %q", test.Bytes, got, want)
				}
			case test.Kind == "line":
				var want truncateFixtureLineResult
				if err := json.Unmarshal(test.Expected, &want); err != nil {
					t.Fatal(err)
				}
				maxChars := GrepMaxLineLength
				if test.MaxChars != nil {
					maxChars = *test.MaxChars
				}
				got := TruncateLine(test.Line, maxChars)
				if got.Text != want.Text || got.WasTruncated != want.WasTruncated {
					t.Errorf("TruncateLine = %+v, want %+v", got, want)
				}
			case test.Kind == "middle":
				var want struct {
					Content      string `json:"content"`
					Truncated    bool   `json:"truncated"`
					RemovedChars int    `json:"removedChars"`
					TotalBytes   int    `json:"totalBytes"`
					TotalLines   int    `json:"totalLines"`
				}
				if err := json.Unmarshal(test.Expected, &want); err != nil {
					t.Fatal(err)
				}
				maxBytes := 0
				if test.MaxBytes != nil {
					maxBytes = *test.MaxBytes
				}
				got := TruncateMiddle(test.Content, maxBytes)
				if got.Content != want.Content || got.Truncated != want.Truncated ||
					got.RemovedChars != want.RemovedChars || got.TotalBytes != want.TotalBytes || got.TotalLines != want.TotalLines {
					t.Errorf("TruncateMiddle = %+v, want %+v", got, want)
				}
			default:
				var want truncateFixtureResult
				if err := json.Unmarshal(test.Expected, &want); err != nil {
					t.Fatal(err)
				}
				options := TruncationOptions{}
				if test.Options.MaxLines != nil {
					options.MaxLines = *test.Options.MaxLines
				}
				if test.Options.MaxBytes != nil {
					options.MaxBytes = *test.Options.MaxBytes
				}
				var got TruncationResult
				if strings.HasPrefix(test.Name, "tail/") {
					got = TruncateTail(test.Content, options)
				} else {
					got = TruncateHead(test.Content, options)
				}
				wantBy := ""
				if want.TruncatedBy != nil {
					wantBy = *want.TruncatedBy
				}
				gotSummary := summarizeTruncation(got)
				wantSummary := summarizeTruncation(TruncationResult{
					Content: want.Content, Truncated: want.Truncated, TruncatedBy: TruncatedBy(wantBy),
					TotalLines: want.TotalLines, TotalBytes: want.TotalBytes,
					OutputLines: want.OutputLines, OutputBytes: want.OutputBytes,
					LastLinePartial: want.LastLinePartial, FirstLineExceedsLimit: want.FirstLineExceedsLimit,
					MaxLines: want.MaxLines, MaxBytes: want.MaxBytes,
				})
				if gotSummary != wantSummary {
					t.Errorf("result = %+v, want %+v (content sha %s vs %s)",
						gotSummary, wantSummary, shortSHA(got.Content), shortSHA(want.Content))
				}
			}
		})
	}
}

// truncateSummary is a comparable projection of a TruncationResult: the
// truncatedBy pointer is compared by value, since JSON null and "lines"/"bytes"
// are the only states.
type truncateSummary struct {
	Content               string
	Truncated             bool
	TruncatedBy           string
	TotalLines            int
	TotalBytes            int
	OutputLines           int
	OutputBytes           int
	LastLinePartial       bool
	FirstLineExceedsLimit bool
	MaxLines              int
	MaxBytes              int
}

func summarizeTruncation(result TruncationResult) truncateSummary {
	return truncateSummary{
		Content: result.Content, Truncated: result.Truncated, TruncatedBy: string(result.TruncatedBy),
		TotalLines: result.TotalLines, TotalBytes: result.TotalBytes,
		OutputLines: result.OutputLines, OutputBytes: result.OutputBytes,
		LastLinePartial: result.LastLinePartial, FirstLineExceedsLimit: result.FirstLineExceedsLimit,
		MaxLines: result.MaxLines, MaxBytes: result.MaxBytes,
	}
}

func shortSHA(value string) string {
	sum := sha256.Sum256([]byte(value))
	return fmt.Sprintf("%x/%d", sum[:6], len(value))
}
