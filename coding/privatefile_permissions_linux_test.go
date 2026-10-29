//go:build linux

package coding

import (
	"os"
	"testing"
)

func TestEnsurePrivateDirectoryRejectsBroadPaths(t *testing.T) {
	for _, path := range []string{".", "..", string(os.PathSeparator)} {
		t.Run(path, func(t *testing.T) {
			before, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := ensurePrivateDirectory(path); err == nil {
				t.Fatalf("ensurePrivateDirectory(%q) succeeded", path)
			}
			after, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			if before.Mode().Perm() != after.Mode().Perm() {
				t.Fatalf("permissions changed from %#o to %#o", before.Mode().Perm(), after.Mode().Perm())
			}
		})
	}
}
