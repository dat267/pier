package tui

import "slices"

// AdoptPreparedFrame installs a private Box's completed render cache on-owner.
// The caller must generation-check the snapshot and ensure its unpadded child
// lines match the current children at the prepared width. No children or user
// callbacks are replaced. The detached snapshot must not be mutated afterward.
// D206: padding and background work can finish on a private worker snapshot.
func (b *Box) AdoptPreparedFrame(prepared *Box) bool {
	if prepared == nil || prepared.cache == nil || b.paddingX != prepared.paddingX || b.paddingY != prepared.paddingY || len(b.Children) != len(prepared.cache.childLines) {
		return false
	}
	hasBg := b.bgFn != nil
	sample := ""
	if hasBg {
		sample = b.bgFn("test")
	}
	if hasBg != prepared.cache.hasBgSample || sample != prepared.cache.bgSample {
		return false
	}
	cache := *prepared.cache
	cache.childComponents = slices.Clone(b.Children)
	cache.childVersions = make([]uint64, len(b.Children))
	layout := make([]mouseChild, len(b.Children))
	for i, child := range b.Children {
		if versioned, ok := child.(renderVersioner); ok {
			if version, has := versioned.RenderVersion(); has {
				cache.childVersions[i] = version
			}
		}
		layout[i] = mouseChild{component: child, height: len(cache.childLines[i])}
	}
	b.cache = &cache
	b.bgSample = sample
	b.bgSampleSet = hasBg
	b.mouseLayout = layout
	b.mouseLayoutWidth = max(1, cache.width-b.paddingX*2)
	return true
}
