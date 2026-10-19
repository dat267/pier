//go:build windows

package coding

import "testing"

// Node's pathToFileURL writes drive paths as file:///C:/... and UNC paths as
// file://host/share/...; the naive "file://"+path form produced invalid URLs.
func TestPathToFileURLWindowsForms(t *testing.T) {
	if got := pathToFileURL(`C:\Users\me\.pi`); got != "file:///C:/Users/me/.pi" {
		t.Fatalf("drive path = %q", got)
	}
	if got := pathToFileURL(`\\server\share\dir`); got != "file://server/share/dir" {
		t.Fatalf("UNC path = %q", got)
	}
}
