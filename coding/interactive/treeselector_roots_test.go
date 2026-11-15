package interactive

import (
	"encoding/json"
	"testing"

	"github.com/dat267/pier/coding"
)

func treeRoot(id, text string) *coding.SessionTreeNode {
	return &coding.SessionTreeNode{Entry: coding.SessionEntry{
		ID: id, Type: "message", Message: json.RawMessage(`{"role":"user","content":"` + text + `"}`),
	}}
}

// TestTreeSelectorPutsTheActiveRootFirst pins pi v1.5.2 (b4afdddf)
// modes/interactive/components/tree-selector.ts flattenTree: `orderedRoots =
// [...roots].sort((a, b) => Number(containsActive.get(b)) -
// Number(containsActive.get(a)))` places the root holding the current leaf
// first, so the branch being read is at the top of the tree view. The port's
// comparator was inverted, so the active root was displayed last.
func TestTreeSelectorPutsTheActiveRootFirst(t *testing.T) {
	roots := []*coding.SessionTreeNode{treeRoot("a", "root a"), treeRoot("b", "root b"), treeRoot("c", "root c")}
	leaf := "c"
	list := NewTreeList(roots, &leaf, 20, nil, FilterAll)
	var ids []string
	for _, node := range list.flatNodes {
		ids = append(ids, node.Node.Entry.ID)
	}
	want := []string{"c", "a", "b"}
	if len(ids) != len(want) {
		t.Fatalf("flattened roots = %v, want %v", ids, want)
	}
	for index := range want {
		if ids[index] != want[index] {
			t.Fatalf("flattened roots = %v, want %v", ids, want)
		}
	}
}
