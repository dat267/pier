//go:build linux

package coding

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// Reference: pi f9a26365 core/session-manager.ts loadEntriesFromFile repairs a
// missing final newline with appendFileSync. It must not rewrite the file: a
// session file that another writer appends to while it is being loaded keeps
// the appended entry.
func TestSessionNewlineRepairKeepsAConcurrentAppend(t *testing.T) {
	loaders := map[string]func(string) error{
		"LoadEntriesFromFile": func(path string) error {
			_, err := LoadEntriesFromFile(path)
			return err
		},
		"LoadEntriesFromFileBuffered": func(path string) error {
			_, _, _, err := LoadEntriesFromFileBuffered(path)
			return err
		},
	}
	for name, load := range loaders {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "session.jsonl")
			var content strings.Builder
			content.WriteString(`{"type":"session","version":3,"id":"s1","timestamp":"t0","cwd":"/tmp"}` + "\n")
			for i := range 20000 {
				fmt.Fprintf(&content, `{"type":"message","id":"m%d","parentId":null,"timestamp":"t","message":{"role":"user","content":"%s"}}`+"\n", i, strings.Repeat("x", 512))
			}
			// The final line has no newline, so the loader repairs it.
			content.WriteString(`{"type":"message","id":"tail","parentId":null,"timestamp":"t","message":{"role":"user","content":"tail"}}`)
			if err := os.WriteFile(path, []byte(content.String()), 0o600); err != nil {
				t.Fatal(err)
			}

			watch, err := unix.InotifyInit1(unix.IN_CLOEXEC)
			if err != nil {
				t.Fatal(err)
			}
			defer unix.Close(watch)
			if _, err := unix.InotifyAddWatch(watch, path, unix.IN_CLOSE_NOWRITE); err != nil {
				t.Fatal(err)
			}
			if err := unix.SetNonblock(watch, true); err != nil {
				t.Fatal(err)
			}

			done := make(chan error, 1)
			go func() { done <- load(path) }()

			// Wait until the loader's read descriptor closes, then append while
			// the loader is still parsing the earlier bytes.
			deadline := time.Now().Add(10 * time.Second)
			buffer := make([]byte, 4096)
			for {
				if time.Now().After(deadline) {
					t.Fatal("loader never closed its read descriptor")
				}
				read, readErr := unix.Read(watch, buffer)
				if readErr == unix.EAGAIN {
					time.Sleep(time.Millisecond)
					continue
				}
				if readErr != nil {
					t.Fatal(readErr)
				}
				if read > 0 {
					break
				}
			}
			marker := `{"type":"message","id":"marker","parentId":null,"timestamp":"t","message":{"role":"user","content":"marker"}}` + "\n"
			file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := file.WriteString(marker); err != nil {
				t.Fatal(err)
			}
			if err := file.Close(); err != nil {
				t.Fatal(err)
			}
			if err := <-done; err != nil {
				t.Fatalf("load: %v", err)
			}
			after, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Contains(after, []byte(`"id":"marker"`)) {
				t.Fatal("newline repair discarded a concurrent append")
			}
		})
	}
}
