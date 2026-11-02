package boundedio

import (
	"errors"
	"io"
	"strings"
	"testing"
)

func TestReadAllAcceptsBodyAtTheLimit(t *testing.T) {
	data, err := ReadAll(strings.NewReader("abcd"), 4)
	if err != nil || string(data) != "abcd" {
		t.Fatalf("data=%q err=%v", data, err)
	}
}

func TestReadAllRejectsBodyOverTheLimit(t *testing.T) {
	data, err := ReadAll(strings.NewReader("abcde"), 4)
	if !errors.Is(err, ErrTooLarge) || data != nil {
		t.Fatalf("data=%q err=%v", data, err)
	}
}

// The reader is asked for one byte past the limit, never the whole body: a peer
// that keeps the connection open must not stall the read.
func TestReadAllStopsAtTheLimit(t *testing.T) {
	reader := &countingReader{data: strings.Repeat("x", 1<<20)}
	if _, err := ReadAll(reader, 16); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("err=%v", err)
	}
	if reader.read > 17 {
		t.Fatalf("read %d bytes, want at most limit+1", reader.read)
	}
}

func TestReadAllWithoutLimitReadsEverything(t *testing.T) {
	data, err := ReadAll(strings.NewReader("abcdef"), 0)
	if err != nil || string(data) != "abcdef" {
		t.Fatalf("data=%q err=%v", data, err)
	}
}

type countingReader struct {
	data string
	read int
}

func (r *countingReader) Read(p []byte) (int, error) {
	if r.read >= len(r.data) {
		return 0, io.EOF
	}
	n := copy(p, r.data[r.read:])
	r.read += n
	return n, nil
}
