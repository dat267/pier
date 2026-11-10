package tui

import (
	"fmt"
	"math/rand"
	"testing"
)

// A stateful fuzz over the alternate screen's input path: a fake terminal
// feeding arbitrary sequences (mouse offsets beyond the viewport, wheel events
// for unknown scroll views, a resized screen, focus changes) must not panic.
// A stale cache or a misclamped event offset would surface as a panic in a
// later frame, and panics report their seed.
func TestAltScreenStatefulFuzz(t *testing.T) {
	terminal := &recordingTerminal{width: 40, height: 10}
	screen := NewAltScreen(terminal, false, t.TempDir(), AltScreenOptions{})
	screen.DisableAutoRender()
	component := &scriptedComponent{lines: []string{"alpha", "beta", "gamma"}}
	screen.AddChild(component)
	screen.Start()
	defer screen.Stop(TuiStopOptions{PreserveScreen: true})

	atoms := []string{
		"\x1b[<64;10;15M", "\x1b[<65;200;1M", "\x1b[<64;400;1M", // wheel, near and far past the edges
		"\x1b[<0;8;4M", "\x1b[<0;8;4m", "\x1b[<32;45;4M", "\x1b[<32;40;40M", // press / motion / release out of range
		"\x1b[<1;12;4M", "\x1b[<1;12;4m", // right button press and release
		"\x1b[<0;99999;-2M", "\x1b[<0;1;2M\x1b[<0;1;2m", // attacker sequences
		"\x1b[I", "\x1b[O", // focus in and out
		"a", "\x1b", "\x1b[B", "\x1b[A", // keys
		"", "\x1b[<0;0;0M",
	}

	for seed := int64(0); seed < 900; seed++ {
		func() {
			defer func() {
				if recovered := recover(); recovered != nil {
					t.Fatalf("seed %d: %v", seed, recovered)
				}
			}()
			rng := rand.New(rand.NewSource(seed))
			for round := 0; round < 40+rng.Intn(60); round++ {
				switch rng.Intn(10) {
				case 0: // resize mid-stream
					terminal.width = 4 + rng.Intn(80)
					terminal.height = 2 + rng.Intn(24)
				case 1: // content change
					component.lines = []string{fmt.Sprintf("line-%d", rng.Intn(1<<20)), "second"}
				default:
					screen.HandleTerminalInput(atoms[rng.Intn(len(atoms))])
				}
				screen.RenderNow(false)
			}
		}()
	}
}
