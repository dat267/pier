package interactive

import (
	"context"
	"testing"
)

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
