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

func indentedLine(rendered, needle string) (string, bool) {
	for _, line := range strings.Split(rendered, "\n") {
		if strings.Contains(line, needle) {
			return line, true
		}
	}
	return "", false
}

// The `!` command output follows outputPad like every other transcript block (upstream #10557).
func TestBashExecutionFollowsOutputPad(t *testing.T) {
	outputPadTheme(t)

	render := func(padding int) string {
		component := NewBashExecutionComponent("ls", nil, false, padding)
		return coding.StripAnsi(strings.Join(component.Render(40), "\n"))
	}
	paddedHeader, ok := indentedLine(render(1), "$ ls")
	if !ok {
		t.Fatal("the padded render has no command header")
	}
	unpaddedHeader, ok := indentedLine(render(0), "$ ls")
	if !ok {
		t.Fatal("the unpadded render has no command header")
	}
	if !strings.HasPrefix(paddedHeader, " ") {
		t.Fatalf("padded header = %q", paddedHeader)
	}
	if strings.HasPrefix(unpaddedHeader, " ") {
		t.Fatalf("unpadded header = %q", unpaddedHeader)
	}

	// A later change re-renders in place, the way the settings walk does it.
	component := NewBashExecutionComponent("ls", nil, false, 0)
	if line, _ := indentedLine(coding.StripAnsi(strings.Join(component.Render(40), "\n")), "$ ls"); strings.HasPrefix(line, " ") {
		t.Fatalf("a zero-padded component rendered indented: %q", line)
	}
	component.SetOutputPad(1)
	if line, _ := indentedLine(coding.StripAnsi(strings.Join(component.Render(40), "\n")), "$ ls"); !strings.HasPrefix(line, " ") {
		t.Fatalf("SetOutputPad did not re-render the header: %q", line)
	}
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
