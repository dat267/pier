package docscheck

import (
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// testFilesManifest lists every `*_test.go` file in the repository, one path per
// line in sorted order.
const testFilesManifest = "internal/docscheck/testfiles.txt"

// TestTestFilesMatchTheBaseline keeps the suite from drifting back to one test
// file per scenario. AGENTS.md (Tests) asks for a unit test file named after the
// production file it covers, so a new file is a decision worth reviewing rather
// than an accident: adding or removing one has to update the manifest in the
// same change. `scripts/test_inventory.sh --write` rewrites it, and the report
// mode prints the per-package file, test and line counts.
func TestTestFilesMatchTheBaseline(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(root, testFilesManifest))
	if err != nil {
		t.Fatalf("read %s: %v", testFilesManifest, err)
	}
	var manifest []string
	for line := range strings.SplitSeq(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		manifest = append(manifest, line)
	}
	if len(manifest) == 0 {
		t.Fatalf("%s is empty; run scripts/test_inventory.sh --write", testFilesManifest)
	}
	if !slices.IsSorted(manifest) {
		t.Error("the manifest is not sorted; run scripts/test_inventory.sh --write")
	}

	present := map[string]bool{}
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			switch entry.Name() {
			case ".git", "pi", "tmp", "bin", "testdata", "node_modules":
				if path != root {
					return filepath.SkipDir
				}
			}
			return nil
		}
		if !strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		present[filepath.ToSlash(rel)] = true
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	if len(present) == 0 {
		t.Fatal("found no test files; the baseline check would be empty")
	}

	for _, file := range manifest {
		if !present[file] {
			t.Errorf("%s is listed but no longer exists; run scripts/test_inventory.sh --write", file)
		}
	}
	listed := make(map[string]bool, len(manifest))
	for _, file := range manifest {
		listed[file] = true
	}
	unlisted := 0
	for file := range present {
		if !listed[file] {
			unlisted++
		}
	}
	if unlisted > 0 {
		// Name a few so the failure leads somewhere without printing hundreds.
		var names []string
		for file := range present {
			if !listed[file] {
				names = append(names, file)
			}
		}
		slices.Sort(names)
		if len(names) > 10 {
			names = names[:10]
		}
		t.Errorf("%d test files are missing from %s (%s); run scripts/test_inventory.sh --write",
			unlisted, testFilesManifest, strings.Join(names, ", "))
	}
}
