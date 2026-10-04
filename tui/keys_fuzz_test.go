package tui

import "testing"

// FuzzKeyParsers checks the key parsers never panic on arbitrary input; the
// seeds cover the encodings terminals send.
func FuzzKeyParsers(f *testing.F) {
	seeds := []string{
		"\x1b[A", "\x1b[1;5C", "\x1b[27;2;13~", "\x1b[97;5u", "\x1b[97;1:2u",
		"\x1b[<0;10;5M", "\x1b[200~paste\x1b[201~", "\x1b\r", "\x08", "a",
		"\x1b[1;2Q", "\x1b]8;;https://x\x07text\x1b]8;;\x07", "\x00\xff",
	}
	for _, seed := range seeds {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, data string) {
		_, _ = ParseKey(data)
		_, _ = DecodeKittyPrintable(data)
		_, _ = DecodePrintableKey(data)
	})
}
