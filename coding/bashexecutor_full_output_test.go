package coding

import (
	"context"
	"os"
	"strings"
	"testing"
)

// linesOnlyBashOperations emits a command's output in one chunk.
type linesOnlyBashOperations struct {
	output string
}

func (operations *linesOnlyBashOperations) Exec(_ context.Context, _, _ string, options BashExecOptions) (*int, error) {
	options.OnData([]byte(operations.output))
	code := 0
	return &code, nil
}

// Reference: pi v1.5.2 (b4afdddf) core/bash-executor.ts ensureTempFile writes
// every buffered chunk into the new file, and finish calls it whenever the tail
// truncates. A command whose output exceeds the line limit but stays under
// DEFAULT_MAX_BYTES therefore gets a full-output file holding all of it. Pier
// created the file without writing the buffered chunks: the write loop only
// runs when new data arrives, so the reported path pointed at an empty file.
func TestExecuteBashWritesTheFullOutputWhenOnlyTheTailTruncates(t *testing.T) {
	var builder strings.Builder
	lineCount := DefaultMaxLines + 500
	for index := 0; index < lineCount; index++ {
		builder.WriteString("line ")
		builder.WriteString(itoa(index))
		builder.WriteString("\n")
	}
	output := builder.String()
	if len(output) >= DefaultMaxBytes {
		t.Fatalf("fixture is %d bytes, want under the %d-byte raw threshold", len(output), DefaultMaxBytes)
	}

	result, err := ExecuteBashWithOperations(context.Background(), "fixture", t.TempDir(),
		&linesOnlyBashOperations{output: output}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Truncated {
		t.Fatalf("result is not truncated for %d lines", lineCount)
	}
	if result.FullOutputPath == "" {
		t.Fatal("no full-output path for a truncated result")
	}
	t.Cleanup(func() { _ = os.Remove(result.FullOutputPath) })

	saved, err := os.ReadFile(result.FullOutputPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(saved) == 0 {
		t.Fatal("full-output file is empty")
	}
	if got := strings.Count(string(saved), "\n"); got != lineCount {
		t.Fatalf("full-output file holds %d lines, want %d", got, lineCount)
	}
	if !strings.HasSuffix(string(saved), "line "+itoa(lineCount-1)+"\n") {
		t.Fatal("full-output file does not end with the last line")
	}
}
