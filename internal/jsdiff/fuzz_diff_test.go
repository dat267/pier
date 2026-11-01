package jsdiff

import "testing"

// FuzzDiffComponentsReconstructsInputs guards the Myers port for arbitrary
// inputs: every old token is consumed exactly once by an equal or removed run,
// every new token by an equal or added run, and the run count stays bounded. A
// violation would silently drop or duplicate rendered diff lines.
func FuzzDiffComponentsReconstructsInputs(f *testing.F) {
	seeds := [][2]string{
		{"", ""}, {"a", ""}, {"", "a"}, {"a:b", "b:a"},
		{"a,b,c", "a,c"}, {"x", "x"}, {"a:a:a", "a"},
	}
	for _, seed := range seeds {
		f.Add(seed[0], seed[1])
	}
	f.Fuzz(func(t *testing.T, oldJoined, newJoined string) {
		oldTokens := splitTokens(oldJoined)
		newTokens := splitTokens(newJoined)
		components := DiffComponents(oldTokens, newTokens, func(a, b string) bool { return a == b })
		oldIndex, newIndex := 0, 0
		for _, component := range components {
			if component.Count < 0 || component.Added && component.Removed {
				t.Fatalf("invalid component %+v for %q / %q", component, oldJoined, newJoined)
			}
			switch {
			case component.Added:
				newIndex += component.Count
			case component.Removed:
				oldIndex += component.Count
			default:
				oldIndex += component.Count
				newIndex += component.Count
			}
		}
		if oldIndex != len(oldTokens) || newIndex != len(newTokens) {
			t.Fatalf("components %+v cover (%d,%d) of (%d,%d) for %q / %q",
				components, oldIndex, newIndex, len(oldTokens), len(newTokens), oldJoined, newJoined)
		}
		if len(components) > len(oldTokens)+len(newTokens)+1 {
			t.Fatalf("unbounded components %d for %q / %q", len(components), oldJoined, newJoined)
		}
	})
}

// splitTokens derives token slices from the fuzz input without allocating a
// grammar: ':' separates tokens and ',,' encodes an empty token.
func splitTokens(joined string) []string {
	if joined == "" {
		return nil
	}
	var tokens []string
	current := ""
	for index := 0; index < len(joined); index++ {
		if joined[index] == ':' {
			tokens = append(tokens, current)
			current = ""
			continue
		}
		current += string(joined[index])
	}
	return append(tokens, current)
}
