package coding

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dat267/pier/ai"
)

// Reference: pi v1.5.0 f9a26365 core/trust-manager.ts writeTrustFile sorts with
// Object.keys(data).sort() and copies every true, false or null value. The
// golden below is that function's output for the same store: Node prints
// JSON.stringify(sorted, null, 2) plus a newline, which keeps the null entry
// and orders keys by UTF-16 code unit (an astral key sorts before U+E000).
func TestProjectTrustStoreWriteMatchesReference(t *testing.T) {
	dir := t.TempDir()
	store := NewProjectTrustStore(dir)
	alpha := filepath.Join(dir, "alpha")
	mid := filepath.Join(dir, "mid")
	added := filepath.Join(dir, "new")
	bmp := filepath.Join(dir, "z\ue000")
	astral := filepath.Join(dir, "z\U0001F600")
	var members []string
	for _, entry := range []struct {
		key   string
		value string
	}{
		{alpha, "true"}, {mid, "null"}, {bmp, "false"}, {astral, "true"},
	} {
		encoded, err := ai.MarshalJSON(entry.key)
		if err != nil {
			t.Fatal(err)
		}
		members = append(members, fmt.Sprintf("%s: %s", encoded, entry.value))
	}
	writeAuthFile(t, store.TrustPath(), "{\n  "+strings.Join(members, ",\n  ")+"\n}\n")

	if err := store.Set(added, new(true)); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(store.TrustPath())
	if err != nil {
		t.Fatal(err)
	}
	want := "{\n"
	for _, entry := range []struct {
		key   string
		value string
	}{
		{alpha, "true"}, {mid, "null"}, {added, "true"}, {astral, "true"}, {bmp, "false"},
	} {
		encoded, err := ai.MarshalJSON(entry.key)
		if err != nil {
			t.Fatal(err)
		}
		want += "  " + string(encoded) + ": " + entry.value + ",\n"
	}
	want = strings.TrimSuffix(want, ",\n") + "\n}\n"
	if string(got) != want {
		t.Fatalf("trust store bytes:\ngot  %q\nwant %q", got, want)
	}
	// The preserved null still reads as "no decision here": resolution walks to
	// the parent instead of treating the entry as a denial.
	if entry, err := store.ReadEntry(mid); err != nil || entry != nil {
		t.Fatalf("null entry resolved to %+v, %v", entry, err)
	}
}
