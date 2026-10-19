package interactive

import (
	"strings"
	"testing"

	"github.com/dat267/pier/coding"
)

func outputPadTheme(t *testing.T) {
	t.Helper()
	installPierThemeForTest(t)
	InitTheme("dark", false)
	t.Cleanup(func() { SetRegisteredThemes(nil) })
}

// The branch and compaction summary blocks follow it too: at pad 1 the block's lines are
// indented, at pad 0 they are not.
func TestSummaryBlocksFollowOutputPad(t *testing.T) {
	outputPadTheme(t)

	indented := func(rendered []string) int {
		count := 0
		for _, line := range rendered {
			if strings.HasPrefix(coding.StripAnsi(line), " ") {
				count++
			}
		}
		return count
	}

	branch := NewBranchSummaryMessageComponent("branched here", nil, 0)
	unpadded := indented(branch.Render(40))
	branch.SetOutputPad(1)
	padded := indented(branch.Render(40))
	if padded <= unpadded {
		t.Fatalf("branch summary indented lines: %d at pad 0, %d at pad 1", unpadded, padded)
	}

	compaction := NewCompactionSummaryMessageComponent("compacted", 1000, nil, 0)
	unpaddedCompaction := indented(compaction.Render(40))
	compaction.SetOutputPad(1)
	paddedCompaction := indented(compaction.Render(40))
	if paddedCompaction <= unpaddedCompaction {
		t.Fatalf("compaction summary indented lines: %d at pad 0, %d at pad 1", unpaddedCompaction, paddedCompaction)
	}
}
