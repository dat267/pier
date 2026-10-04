package durable

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// FuzzJsonlRecovery checks the JSONL storage's recovery never panics on an
// arbitrary main log: a malformed log may fail to open, but must not crash.
func FuzzJsonlRecovery(f *testing.F) {
	seeds := []string{
		"",
		"\n",
		"not json\n",
		`{"v":1,"seq":1,"kind":"commit","writes":[]}` + "\n",
		`{"seq":1,"writes":[{"type":"conversation","record":{"id":1}}]}` + "\n",
		`{"seq":1,"writes":[]}` + "\n{\"seq\":2\"}\n",
		"\x00\xff\xfe",
	}
	for _, seed := range seeds {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, log string) {
		dir, err := os.MkdirTemp("", "pier-jsonl-fuzz")
		if err != nil {
			t.Skip(err)
		}
		defer os.RemoveAll(dir)
		if err := os.WriteFile(filepath.Join(dir, jsonlMainFile), []byte(log), 0o600); err != nil {
			t.Skip(err)
		}
		ctx := context.Background()
		storage, err := OpenJsonlStorage(ctx, dir, JsonlStorageOptions{})
		if err != nil {
			return
		}
		_, _ = storage.ScanConversations(ctx, ConversationQuery{}, nil, 10)
		_, _ = storage.ScanEntries(ctx, EntryQuery{}, nil, 10)
		_, _ = storage.MintID(ctx)
		_ = storage.Close(ctx)
	})
}
