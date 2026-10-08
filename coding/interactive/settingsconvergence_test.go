package interactive

import (
	"context"
	"testing"
)

// Upstream interactive-mode.ts's handleReloadCommand guards transcript
// restoration with chatRestoredBeforeSessionStart, rebuilding exactly once.
func TestSettingsReloadRebuildsTranscriptOnce(t *testing.T) {
	for _, streaming := range []bool{false, true} {
		name := "idle"
		if streaming {
			name = "streaming"
		}
		t.Run(name, func(t *testing.T) {
			app, cleanup := newTestApp(t)
			defer cleanup()
			defer app.Close()
			app.Init(context.Background())
			app.settingsW.Session = &settingsTestSession{streaming: streaming}
			app.settings.SetOutputPad(0)
			app.sessionMgr.AppendCustomEntry("reload-probe", nil)
			rebuilt := 0
			app.transcript.EntryRenderer = func(customType string) EntryRenderer {
				if customType == "reload-probe" {
					rebuilt++
				}
				return nil
			}

			app.applyReloadedSettings()
			if rebuilt != 1 {
				t.Fatalf("reload rendered the transcript entry %d times, want 1", rebuilt)
			}
			if app.display.OutputPad != 0 {
				t.Fatalf("output pad = %d, want 0 after reload", app.display.OutputPad)
			}
		})
	}
}

// The /settings callback and /reload apply a setting through the same
// owner-loop routine. Before, reload only set Display.OutputPad and left the
// rendered messages stale; now it refreshes them like the selector callback
// does.
func TestSettingsReloadAppliesOutputPadThroughTheSharedRoutine(t *testing.T) {
	app, cleanup := newTestApp(t)
	defer cleanup()
	app.Init(context.Background())

	rebuilt := 0
	app.settingsW.RebuildChatFromMessages = func() { rebuilt++ }
	app.settings.SetOutputPad(1)
	app.applySettingsDependentUI()
	if app.display.OutputPad != 1 {
		t.Fatalf("output pad = %d, want 1", app.display.OutputPad)
	}
	if rebuilt == 0 {
		t.Fatal("reload did not refresh the transcript for the new output padding")
	}
}
