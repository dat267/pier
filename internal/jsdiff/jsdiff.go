// Package jsdiff is jsdiff v8's Myers diff core, shared by the word-diff
// renderer and the unified-patch generator. The npm package is unavailable
// offline, so the algorithm is ported from its source (D87) and verified
// against the real library through Node goldens.
package jsdiff

// Component is one run of equal, added or removed tokens in diff order.
type Component struct {
	Count   int
	Added   bool
	Removed bool
}

// componentNode is the linked list the search paths share; a path appends a
// node per run instead of copying the whole history.
type componentNode struct {
	count    int
	added    bool
	removed  bool
	previous *componentNode
}

type searchPath struct {
	oldPos int
	last   *componentNode
}

// DiffComponents runs the jsdiff Myers search over tokens with the given
// equality, returning merged runs in forward order (the jsdiff components
// buildValues consumes). The diagonal-pruning bounds match jsdiff.
func DiffComponents(oldTokens []string, newTokens []string, equal func(a, b string) bool) []Component {
	newLen := len(newTokens)
	oldLen := len(oldTokens)

	bestPath := map[int]*searchPath{0: {oldPos: -1}}
	editLength := 1
	maxEditLength := newLen + oldLen
	minDiagonalToConsider := -1 << 30
	maxDiagonalToConsider := 1 << 30

	extractCommon := func(basePath *searchPath, diagonalPath int) int {
		oldPos := basePath.oldPos
		newPos := oldPos - diagonalPath
		commonCount := 0
		for newPos+1 < newLen && oldPos+1 < oldLen && equal(oldTokens[oldPos+1], newTokens[newPos+1]) {
			newPos++
			oldPos++
			commonCount++
		}
		if commonCount > 0 {
			basePath.last = &componentNode{count: commonCount, previous: basePath.last}
		}
		basePath.oldPos = oldPos
		return newPos
	}

	addToPath := func(path *searchPath, added bool, removed bool, oldPosInc int) *searchPath {
		last := path.last
		if last != nil && last.added == added && last.removed == removed {
			return &searchPath{
				oldPos: path.oldPos + oldPosInc,
				last: &componentNode{
					count: last.count + 1, added: added, removed: removed, previous: last.previous,
				},
			}
		}
		return &searchPath{
			oldPos: path.oldPos + oldPosInc,
			last:   &componentNode{count: 1, added: added, removed: removed, previous: last},
		}
	}

	newPos := extractCommon(bestPath[0], 0)
	if bestPath[0].oldPos+1 >= oldLen && newPos+1 >= newLen {
		return collectComponents(bestPath[0].last)
	}

	for editLength <= maxEditLength {
		found := false
		var foundPath *searchPath
		start := max(minDiagonalToConsider, -editLength)
		end := min(maxDiagonalToConsider, editLength)
		for diagonalPath := start; diagonalPath <= end; diagonalPath += 2 {
			removePath := bestPath[diagonalPath-1]
			addPath := bestPath[diagonalPath+1]
			if removePath != nil {
				delete(bestPath, diagonalPath-1)
			}
			canAdd := false
			if addPath != nil {
				addPathNewPos := addPath.oldPos - diagonalPath
				canAdd = addPathNewPos >= 0 && addPathNewPos < newLen
			}
			canRemove := removePath != nil && removePath.oldPos+1 < oldLen
			if !canAdd && !canRemove {
				delete(bestPath, diagonalPath)
				continue
			}

			var basePath *searchPath
			if !canRemove || (canAdd && removePath.oldPos < addPath.oldPos) {
				basePath = addToPath(addPath, true, false, 0)
			} else {
				basePath = addToPath(removePath, false, true, 1)
			}
			newPos = extractCommon(basePath, diagonalPath)
			if basePath.oldPos+1 >= oldLen && newPos+1 >= newLen {
				found = true
				foundPath = basePath
				break
			}
			bestPath[diagonalPath] = basePath
			if basePath.oldPos+1 >= oldLen {
				maxDiagonalToConsider = min(maxDiagonalToConsider, diagonalPath-1)
			}
			if newPos+1 >= newLen {
				minDiagonalToConsider = max(minDiagonalToConsider, diagonalPath+1)
			}
		}
		if found {
			return collectComponents(foundPath.last)
		}
		editLength++
	}
	return nil
}

func collectComponents(last *componentNode) []Component {
	var nodes []*componentNode
	for last != nil {
		nodes = append(nodes, last)
		last = last.previous
	}
	components := make([]Component, len(nodes))
	for index, node := range nodes {
		components[len(nodes)-1-index] = Component{Count: node.count, Added: node.added, Removed: node.removed}
	}
	return components
}
