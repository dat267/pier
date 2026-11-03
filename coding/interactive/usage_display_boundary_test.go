package interactive

import (
	"strings"
	"testing"

	"github.com/dat267/pier/ai"
	"github.com/dat267/pier/coding"
)

// D369 saturates session usage columns. The display layer must not add those
// saturated columns back in int64 and render a negative total.
func TestSessionInfoSaturatesHugeTokenColumns(t *testing.T) {
	wiring, session, _ := newCommandTestWiring(t)
	maxInt64 := int64(^uint64(0) >> 1)
	session.stats.Tokens.Input = maxInt64
	session.stats.Tokens.CacheRead = maxInt64
	session.stats.Tokens.CacheWrite = maxInt64
	session.stats.Tokens.Total = maxInt64
	wiring.HandleSessionCommand(1000)
	lines := coding.StripAnsi(strings.Join(wiring.Chat.Render(120), "\n"))
	for _, want := range []string{
		"Input: " + formatThousands(maxInt64),
		"Uncached: " + formatThousands(maxInt64),
	} {
		if !strings.Contains(lines, want) {
			t.Errorf("missing %q in:\n%s", want, lines)
		}
	}
}

func TestCompactionCostNoticeSaturatesHugeUsage(t *testing.T) {
	renderer, settings := newTranscriptTestRenderer(t, true)
	settings.SetShowCacheMissNotices(true)
	maxInt64 := int64(^uint64(0) >> 1)
	renderer.AddCompactionCostNotice(CompactionCostNotice{Kind: "compaction", Usage: ai.Usage{
		Input: maxInt64, Output: maxInt64, CacheRead: maxInt64, CacheWrite: maxInt64,
	}})
	lines := strings.Join(renderChat(t, renderer.Chat), "\n")
	want := "Compaction: " + FormatTokens(maxInt64) + " tokens billed"
	if !strings.Contains(lines, want) {
		t.Errorf("missing %q in:\n%s", want, lines)
	}
}
