package markdown

import "testing"

// Reference: pi v1.5.2 (b4afdddf) tui/src/components/markdown.ts with marked
// 18.0.11. A table body continues over every line that does not start another
// block, and the row rule is the paragraph-interrupt test: an ordered marker
// other than `1.` cannot interrupt a paragraph, so `5. item` becomes a row,
// while `1.`, `-`, `*`, `+`, a heading, a fence, a quote or an indented line
// ends the table.
func TestTableBodyContinuesOverANonInterruptingListLine(t *testing.T) {
	blocks := Lex("| a | b |\n| - | - |\n5. item\n")
	if len(blocks) != 1 || blocks[0].Kind != BlockTable {
		t.Fatalf("blocks = %d, first kind %v; want one table", len(blocks), blocks[0].Kind)
	}
	if len(blocks[0].Rows) != 1 || len(blocks[0].Rows[0]) != 2 ||
		blocks[0].Rows[0][0] != "5. item" || blocks[0].Rows[0][1] != "" {
		t.Fatalf("rows = %q, want one row holding %q", blocks[0].Rows, "5. item")
	}

	for _, line := range []string{"1. item", "- item", "* item", "+ item", "# head", "```", "> quote", "    code", "---"} {
		blocks := Lex("| a | b |\n| - | - |\n" + line + "\n")
		if len(blocks) < 2 {
			t.Fatalf("%q: blocks = %d, want the table plus a following block", line, len(blocks))
		}
		if blocks[0].Kind != BlockTable || len(blocks[0].Rows) != 0 {
			t.Fatalf("%q: first block kind %v with %d rows, want an empty table", line, blocks[0].Kind, len(blocks[0].Rows))
		}
	}
}
