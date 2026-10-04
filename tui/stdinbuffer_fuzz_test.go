package tui

import (
	"testing"
	"time"
)

// FuzzStdinBuffer checks the terminal input splitter never panics on arbitrary
// bytes and deadlines.
func FuzzStdinBuffer(f *testing.F) {
	seeds := []string{
		"\x1b[A", "\x1b[1;5C", "\x1b[<0;10;5M", "\x1b[200~paste\x1b[201~",
		"\x1b]8;;https://x\x07text\x1b]8;;\x07", "\x1b", "\x1b\r", "\x00\xff", "",
		"\x1b[M !!", "\x1b[97;5u", "\x1b[1;2Q", "\x1b\x1b[1;1R",
	}
	for _, seed := range seeds {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, input string) {
		buffer := NewStdinBuffer(StdinBufferOptions{Timeout: 10, EscapeTimeout: 10})
		buffer.OnData = func(string) {}
		buffer.ProcessString(input)
		_ = buffer.FlushExpired(time.Now().Add(time.Hour))
		// A second chunk after a flush exercises the flushed-head re-join path.
		buffer.ProcessString(input)
		_ = buffer.FlushExpired(time.Now().Add(2 * time.Hour))
	})
}
