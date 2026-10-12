//go:build linux

package coding

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// Upstream write.ts uses fs.mkdir({recursive:true}) and fs.writeFile with
// Node's default modes, 0777 for directories and 0666 for files before umask.
func TestWriteToolUsesNodeDefaultCreationModes(t *testing.T) {
	oldMask := syscall.Umask(0)
	defer syscall.Umask(oldMask)

	root := t.TempDir()
	tool := CreateWriteTool(root)
	_, err := tool.Execute("c", json.RawMessage(`{"path":"nested/file.txt","content":"x"}`), context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}

	fileInfo, err := os.Stat(filepath.Join(root, "nested", "file.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if got := fileInfo.Mode().Perm(); got != 0o666 {
		t.Errorf("file mode = %#o; want Node default %#o", got, 0o666)
	}
	dirInfo, err := os.Stat(filepath.Join(root, "nested"))
	if err != nil {
		t.Fatal(err)
	}
	if got := dirInfo.Mode().Perm(); got != 0o777 {
		t.Errorf("directory mode = %#o; want Node default %#o", got, 0o777)
	}

}
