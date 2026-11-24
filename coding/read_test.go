package coding

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/dat267/pier/ai"
)

// Reference: pi v1.4.0 utils/mime.ts accepts a zero-byte sniff;
// core/tools/read.ts returns an empty text block for an empty file.
func TestReadEmptyTextFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.txt")
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	result := execTool(t, CreateReadTool(dir, nil), `{"path":"empty.txt"}`)
	if len(result.Content) != 1 || result.Content[0].(ai.TextContent).Text != "" {
		t.Fatalf("result = %+v", result)
	}
}

func readToolText(t *testing.T, dir string, params string) (string, error) {
	t.Helper()
	result, err := CreateReadTool(dir, nil).Execute(context.Background(), "id", json.RawMessage(params), nil)
	if err != nil {
		return "", err
	}
	return result.Content[0].(ai.TextContent).Text, nil
}

// read.ts checks `userLimitedLines !== undefined`, so a negative limit (the
// schema is Type.Number) still gets the remaining-lines notice, counted from
// startLine + limit; the expected texts were produced by the upstream text
// path (pi v1.5.0 packages/coding-agent/src/core/tools/read.ts).
func TestReadToolNegativeLimitStillReportsRemainingLines(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("a\nb\nc"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ params, want string }{
		{`{"path":"f.txt","offset":1,"limit":-1}`, "a\nb\n\n[4 more lines in file. Use offset=0 to continue.]"},
		{`{"path":"f.txt","offset":3,"limit":-3}`, "\n\n[4 more lines in file. Use offset=0 to continue.]"},
		{`{"path":"f.txt","limit":0}`, "\n\n[3 more lines in file. Use offset=1 to continue.]"},
	} {
		got, err := readToolText(t, dir, tc.params)
		if err != nil || got != tc.want {
			t.Errorf("%s: got %q, %v; want %q", tc.params, got, err, tc.want)
		}
	}
}

// D400: read.ts indexes allLines[startLine] with a fractional startLine for
// the oversized-first-line notice, so Buffer.byteLength(undefined) throws a
// TypeError. Pier reports the line's size from the slice it actually read.
func TestReadToolFractionalOffsetReportsOversizedLine(t *testing.T) {
	dir := t.TempDir()
	content := "a\n" + strings.Repeat("y", 52000) + "\n"
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := readToolText(t, dir, `{"path":"f.txt","offset":2.5}`)
	want := "[Line 2.5 is 50.8KB, exceeds 50.0KB limit. Use bash: sed -n '2.5p' f.txt | head -c 51200]"
	if err != nil || got != want {
		t.Fatalf("got %q, %v; want %q", got, err, want)
	}
}

// read.ts rejects an offset past the last line and shows the remaining-lines
// notice only when lines actually remain. Both are boundary conditions: the
// check is `startLine >= totalLines` and the notice needs
// `startLine + userLimitedLines < totalLines`.
func TestReadToolOffsetAndNoticeBoundaries(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "two.txt"), []byte("a\nb"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Offset 3 in a two-line file starts past the end.
	if _, err := readToolText(t, dir, `{"path":"two.txt","offset":3}`); err == nil || !strings.Contains(err.Error(), "beyond end of file") {
		t.Fatalf("offset 3 on a two-line file: err = %v, want beyond end of file", err)
	}
	// Reading exactly to the last line leaves nothing to continue.
	got, err := readToolText(t, dir, `{"path":"two.txt","offset":1,"limit":2}`)
	if err != nil {
		t.Fatal(err)
	}
	if got != "a\nb" {
		t.Fatalf("read to EOF = %q, want the two lines with no remaining-lines notice", got)
	}
}

// Reference: pi v1.4.0 core/tools/read.ts defaults omitted autoResizeImages to true.
func TestReadModelOptionKeepsDefaultResize(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "wide.png"), solidPNG(t, 2001, 1), 0600); err != nil {
		t.Fatal(err)
	}
	result := execTool(t, CreateReadTool(dir, &ReadToolOptions{Model: func() *ai.Model { return nil }}), `{"path":"wide.png"}`)
	data, err := base64.StdEncoding.DecodeString(result.Content[1].(ai.ImageContent).Data)
	if err != nil {
		t.Fatal(err)
	}
	size, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if size.Width != 2000 {
		t.Fatalf("width=%d; want 2000", size.Width)
	}
}

// pi v1.4.0 (4a080252), core/tools/read.ts text path with default operations.
// Regenerate:
//
//	node --experimental-strip-types coding/testdata/read-tool/generate.mjs ./pi > coding/testdata/read-tool/golden.json
func TestReadToolMatchesUpstreamTextPath(t *testing.T) {
	raw, err := os.ReadFile("testdata/read-tool/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Name     string   `json:"name"`
		Path     string   `json:"path"`
		Offset   *float64 `json:"offset"`
		Limit    *float64 `json:"limit"`
		Expected struct {
			Text    string          `json:"text"`
			Details json.RawMessage `json:"details"`
			Error   string          `json:"error"`
		} `json:"expected"`
	}
	if err := json.Unmarshal(raw, &fixtures); err != nil {
		t.Fatal(err)
	}
	if len(fixtures) == 0 {
		t.Fatal("empty read-tool corpus")
	}
	dir := t.TempDir()
	files := map[string][]byte{
		"empty.txt":                        {},
		"three.txt":                        []byte("one\ntwo\nthree\n"),
		"no-newline.txt":                   []byte("one\ntwo"),
		"blank-lines.txt":                  []byte("a\n\n\nb\n"),
		"crlf.txt":                         []byte("a\r\nb\r\n"),
		"bom.txt":                          []byte("\uFEFFfirst\nsecond\n"),
		"invalid.txt":                      {0xff, 0x0a, 'A', 0x0a},
		"long-line.txt":                    []byte(strings.Repeat("x", 60000) + "\nsecond\n"),
		"many-lines.txt":                   []byte(manyLines(2500)),
		"wide.txt":                         []byte(strings.Repeat("日本語 line\n", 12000)),
		filepath.Join("sub", "nested.txt"): []byte("nested\n"),
	}
	for name, content := range files {
		full := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, content, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	tool := CreateReadTool(dir, nil)
	for _, fixture := range fixtures {
		t.Run(fixture.Name, func(t *testing.T) {
			params := map[string]any{"path": fixture.Path}
			if fixture.Offset != nil {
				params["offset"] = *fixture.Offset
			}
			if fixture.Limit != nil {
				params["limit"] = *fixture.Limit
			}
			encoded, err := json.Marshal(params)
			if err != nil {
				t.Fatal(err)
			}
			result, execErr := tool.Execute(context.Background(), "call", encoded, nil)
			if !checkParityToolResult(t, result, execErr, fixture.Expected.Error, fixture.Expected.Text, fixture.Expected.Details) {
				return
			}
		})
	}
}

func TestReadToolMissingFileError(t *testing.T) {
	dir := t.TempDir()
	_, err := CreateReadTool(dir, nil).Execute(context.Background(), "call", []byte(`{"path":"nope.txt"}`), nil)
	if err == nil {
		t.Fatal("reading a missing file succeeded")
	}
	message := err.Error()
	if !strings.Contains(message, "no such file or directory") || !strings.Contains(message, "nope.txt") {
		t.Fatalf("error = %q, want Go's os error naming the path", message)
	}
	if strings.HasPrefix(message, "ENOENT:") {
		t.Fatalf("error = %q, want the port's own wording (D279)", message)
	}
}

// writeReadFixture writes a file of the given size made of fixed-width lines and
// returns the number of lines it holds.
func writeReadFixture(t *testing.T, path string, size int64) int {
	t.Helper()
	line := []byte(strings.Repeat("x", 62) + "\n")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	chunk := bytes.Repeat(line, 1024)
	var written int64
	for written < size {
		if _, err := file.Write(chunk); err != nil {
			t.Fatal(err)
		}
		written += int64(len(chunk))
	}
	return int(written / int64(len(line)))
}

// pi v1.5.2 (b4afdddf) core/tools/read.ts streams text files and keeps only the
// lines it shows, so reading costs memory in proportion to the output. Before
// that the tool loaded and split the whole file, so one read of a large log held
// several times its size on the heap at once.
func TestReadToolKeepsMemoryBoundedOnALargeFile(t *testing.T) {
	dir := t.TempDir()
	const size = 32 << 20
	lines := writeReadFixture(t, filepath.Join(dir, "big.log"), size)
	params := `{"path":"big.log","limit":10}`
	if _, err := readToolText(t, dir, params); err != nil {
		t.Fatal(err)
	}

	// Sample the live heap while the read runs: the read itself only garbage,
	// so the allocation counters cannot tell the two implementations apart.
	runtime.GC()
	var baseline runtime.MemStats
	runtime.ReadMemStats(&baseline)
	stop := make(chan struct{})
	peak := make(chan uint64, 1)
	go func() {
		high := baseline.HeapAlloc
		for {
			select {
			case <-stop:
				peak <- high
				return
			default:
			}
			var stats runtime.MemStats
			runtime.ReadMemStats(&stats)
			high = max(high, stats.HeapAlloc)
			time.Sleep(200 * time.Microsecond)
		}
	}()

	text, err := readToolText(t, dir, params)
	close(stop)
	high := <-peak
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(text, "x") {
		t.Fatalf("output does not start with the first line: %q", text[:min(len(text), 80)])
	}
	// The file ends with a newline, so split("\n") counts one more, empty line.
	if want := fmt.Sprintf("[%d more lines in file. Use offset=11 to continue.]", lines+1-10); !strings.Contains(text, want) {
		t.Fatalf("output = %q; want the remaining-lines notice %q", text, want)
	}

	const budget = 24 << 20
	if growth := high - baseline.HeapAlloc; growth > budget {
		t.Fatalf("reading %d MiB to show 10 lines peaked %d MiB above the baseline heap, want under %d MiB",
			size>>20, growth>>20, budget>>20)
	}
}
