//go:build windows

package coding

import "testing"

// Upstream find.ts uses path.relative with the platform-native Windows path module.
func TestRelativizeFindResultPathWindowsAbsolutePath(t *testing.T) {
	got := RelativizeFindResultPath(`C:\workspace\sub\file\`, `C:\workspace`)
	if got != "sub/file/" {
		t.Fatalf("relativized Windows path = %q; want %q", got, "sub/file/")
	}
}
