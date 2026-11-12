package ai

import (
	"io"
	"strings"
	"testing"
)

// oneByteReader returns one byte per Read so the SSE splitter is driven at the
// worst possible chunk boundaries.
type oneByteReader struct {
	data []byte
}

func (r *oneByteReader) Read(p []byte) (int, error) {
	if len(r.data) == 0 {
		return 0, io.EOF
	}
	if len(p) == 0 {
		return 0, nil
	}
	p[0] = r.data[0]
	r.data = r.data[1:]
	return 1, nil
}

func scanAll(reader io.Reader) ([]string, error) {
	scanner := newSSEScanner(reader)
	var lines []string
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	return lines, scanner.Err()
}

// FuzzSSELineSplitter checks that the CR/LF/CRLF splitter produces the same
// lines whether the provider's stream arrives in one read or one byte per read.
// Its skipLF state crosses read boundaries, so a boundary bug would drop or
// duplicate a line mid-stream.
func FuzzSSELineSplitter(f *testing.F) {
	for _, seed := range []string{
		"", "a", "a\n", "a\r", "a\r\n", "a\rb", "a\r\r\nb", "\r\n\r\n", "data: 1\n\ndata: 2\n\n",
		"a\rb\nc\r\nd", "\n\r", "\r", "\r\n", "x\r\n\r", "x\r\r",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, input string) {
		want, wantErr := scanAll(strings.NewReader(input))
		got, gotErr := scanAll(&oneByteReader{data: []byte(input)})
		if (wantErr != nil) != (gotErr != nil) {
			t.Fatalf("input %q: one-shot err = %v, byte-at-a-time err = %v", input, wantErr, gotErr)
		}
		if len(want) != len(got) {
			t.Fatalf("input %q: lines = %q one-shot, %q byte-at-a-time", input, want, got)
		}
		for index := range want {
			if want[index] != got[index] {
				t.Fatalf("input %q: line %d = %q one-shot, %q byte-at-a-time", input, index, want[index], got[index])
			}
		}
	})
}
