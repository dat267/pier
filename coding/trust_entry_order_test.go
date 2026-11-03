package coding

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dat267/pier/ai"
)

// Reference: pi v1.5.0 f9a26365 core/trust-manager.ts findTrustFileKeys and
// findNearestTrustEntry select the first matching boolean in Object.keys order.
// Equivalent path spellings must not turn a stored denial into a random grant.
func TestProjectTrustStoreUsesDocumentOrderForEquivalentKeys(t *testing.T) {
	dir := t.TempDir()
	store := NewProjectTrustStore(dir)
	project := filepath.Join(dir, "project")
	if err := os.Mkdir(project, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, firstDecision := range []bool{false, true} {
		var members []string
		for i := 7; i >= 0; i-- {
			key := project + "/" + strings.Repeat("./", i)
			encoded, err := ai.MarshalJSON(key)
			if err != nil {
				t.Fatal(err)
			}
			decision := !firstDecision
			if i == 7 {
				decision = firstDecision
			}
			members = append(members, fmt.Sprintf("%s:%t", encoded, decision))
		}
		writeAuthFile(t, store.TrustPath(), "{"+strings.Join(members, ",")+"}")
		// Repeated reads pin stability across Go's randomized map traversal.
		for range 16 {
			entry, err := store.ReadEntry(project)
			if err != nil || entry == nil || entry.Decision != firstDecision {
				t.Fatalf("first decision %t: entry=%+v, err=%v", firstDecision, entry, err)
			}
		}
	}
	if err := store.Set(project, new(false)); err != nil {
		t.Fatal(err)
	}
	entry, err := store.ReadEntry(project)
	if err != nil || entry == nil || entry.Decision {
		t.Fatalf("single-key replacement control: %+v, %v", entry, err)
	}
}
