package interactive

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/dat267/pier/coding"
)

func newTreeSearchFixture(t *testing.T, count int) []*coding.SessionTreeNode {
	t.Helper()
	nodes := make([]*coding.SessionTreeNode, count)
	var parent *string
	for index := range nodes {
		id := fmt.Sprintf("e%05d", index)
		content, err := json.Marshal([]map[string]string{{"type": "text", "text": "message body number " + id + " needle"}})
		if err != nil {
			t.Fatal(err)
		}
		message, err := json.Marshal(map[string]any{"role": "assistant", "content": json.RawMessage(content)})
		if err != nil {
			t.Fatal(err)
		}
		nodes[index] = &coding.SessionTreeNode{Entry: coding.SessionEntry{
			Type: "message", ID: id, ParentID: parent, Message: message,
			Timestamp: "2026-01-01T00:00:00.000Z",
		}}
		parent = &nodes[index].Entry.ID
	}
	return nodes
}

// M66: the search box filters on every keystroke. Decoding and lowercasing each
// entry once and reusing it keeps that pass to the text scan instead of a JSON
// decode of the whole transcript (measured 22 allocations per node before).
func TestTreeSearchReusesPerNodeWork(t *testing.T) {
	InitTheme("dark", false)
	const nodes = 500
	list := NewTreeList(newTreeSearchFixture(t, nodes), nil, 20, nil, FilterDefault)
	list.searchQuery = "needle"
	list.ApplyFilter()

	allocs := testing.AllocsPerRun(3, func() { list.ApplyFilter() })
	if allocs > float64(nodes*4) {
		t.Fatalf("repeated search allocated %.0f times over %d nodes, want at most %d", allocs, nodes, nodes*4)
	}
	if got := len(list.filteredNodes); got != nodes {
		t.Fatalf("filtered nodes = %d, want %d", got, nodes)
	}
}

// Filtering results must not depend on whether the cache is warm, across filter
// modes, queries, and label changes.
func TestTreeSearchCacheMatchesUncachedFiltering(t *testing.T) {
	InitTheme("dark", false)
	tree := newTreeSearchFixture(t, 40)
	list := NewTreeList(tree, nil, 20, nil, FilterDefault)
	queries := []string{"", "needle", "e0001", "missing", "MESSAGE"}
	modes := []FilterMode{FilterDefault, FilterUserOnly, FilterNoTools, FilterLabeledOnly, FilterAll}
	for _, mode := range modes {
		list.filterMode = mode
		for _, query := range queries {
			list.searchQuery = query
			list.ApplyFilter()
			warm := treeIDs(list.filteredNodes)

			// A fresh list has no cached nodes; it must agree.
			fresh := NewTreeList(tree, nil, 20, nil, FilterDefault)
			fresh.filterMode = mode
			fresh.searchQuery = query
			fresh.ApplyFilter()
			if cold := treeIDs(fresh.filteredNodes); len(cold) != len(warm) || (len(cold) > 0 && cold[0] != warm[0]) {
				t.Fatalf("mode %q query %q: cached=%v uncached=%v", mode, query, warm, cold)
			}
		}
	}

	// A label is part of the searchable text, so labeling must refresh the cache.
	list.searchQuery = "release-candidate"
	list.filterMode = FilterLabeledOnly
	list.ApplyFilter()
	if len(list.filteredNodes) != 0 {
		t.Fatalf("label filter matched before labeling: %d", len(list.filteredNodes))
	}
	label := "release-candidate"
	list.UpdateNodeLabel(tree[7].Entry.ID, &label, "")
	list.ApplyFilter()
	if len(list.filteredNodes) != 1 || list.filteredNodes[0].Node.Entry.ID != tree[7].Entry.ID {
		t.Fatalf("labeled node not found after cache refresh: %d", len(list.filteredNodes))
	}
}

func treeIDs(nodes []*flatNode) []string {
	ids := make([]string, len(nodes))
	for index, node := range nodes {
		ids[index] = node.Node.Entry.ID
	}
	return ids
}
