//go:build linux

package coding

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// clipboard.ts decodes command output with Buffer.toString("utf8").
func TestReadClipboardTextReplacesMalformedUTF8(t *testing.T) {
	stubDir := t.TempDir()
	stub := filepath.Join(stubDir, "wl-paste")
	if err := os.WriteFile(stub, []byte("#!/bin/sh\nprintf '\\377'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TERMUX_VERSION", "")
	t.Setenv("DISPLAY", "")
	t.Setenv("WAYLAND_DISPLAY", "wayland-0")
	t.Setenv("PATH", stubDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	got, err := ReadClipboardTextContext(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got != "\uFFFD" {
		t.Fatalf("clipboard = %q; want replacement-decoded text", got)
	}
}
