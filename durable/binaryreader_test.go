package durable

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestRangeUTF8DecoderCarriesSplitRune(t *testing.T) {
	decoder := newRangeUTF8Decoder()
	parts := [][]byte{{0xe2}, {0x82}, {0xac}}
	wants := []string{"", "", "€"}
	for index, part := range parts {
		text, err := decoder.Feed(part)
		if err != nil {
			t.Fatal(err)
		}
		if string(text) != wants[index] {
			t.Fatalf("decoded chunk = %q, want %q", text, wants[index])
		}
	}
	if tail, err := decoder.Finish(); err != nil || len(tail) != 0 {
		t.Fatalf("finish = %q, %v", tail, err)
	}
}

func TestOSBinaryReaderReadsAndScansSelectedLines(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	cwd := t.TempDir()
	fsys, err := NewOSFileSystem(cwd)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(cwd, "lines.txt")
	content := []byte{0xef, 0xbb, 0xbf, 'a', '\n', 0xc3, 0xa9, '\n', 'l', 'a', 's', 't'}
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	reader, err := fsys.OpenBinaryReader(path, ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close(ctx)
	info, err := reader.Info(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size != int64(len(content)) {
		t.Fatalf("size = %d, want %d", info.Size, len(content))
	}
	bytes, err := reader.Read(5, 2, ctx)
	if err != nil || string(bytes) != "é" {
		t.Fatalf("read = %q, %v", bytes, err)
	}

	end := int64(2)
	scan, err := reader.ScanLines(1, &end, ctx)
	if err != nil {
		t.Fatal(err)
	}
	if scan.Newlines != 2 || scan.Start != 5 || scan.End != 7 || scan.FirstLineEnd != 7 ||
		scan.LastLineStart != 5 || scan.SelectedBytes != 2 || scan.FirstLineBytes != 2 {
		t.Fatalf("scan = %+v", scan)
	}

	scan, err = reader.ScanLines(1, nil, ctx)
	if err != nil {
		t.Fatal(err)
	}
	if scan.Start != 5 || scan.End != int64(len(content)) || scan.LastLineStart != 8 ||
		scan.SelectedBytes != 7 || scan.FirstLineBytes != 2 {
		t.Fatalf("open-ended scan = %+v", scan)
	}
}
