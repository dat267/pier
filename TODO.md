# Reproduced, unresolved findings

## The first-run setup dialog is reachable only from its golden test (open)

Upstream `main.ts:719` runs `showFirstTimeSetup(startupSettingsManager)` when
`shouldRunFirstTimeSetup()` (interactive, no `--help`, no `--list-models`). It is
how a new install picks a theme and opts in or out of analytics, and it writes
`enableAnalytics`/`trackingId` through the settings manager.

pier has no `shouldRunFirstTimeSetup` and no `showFirstTimeSetup`: `cmd/main.go`
never calls them, so `NewFirstTimeSetupComponent` (`coding/interactive/eastereggs.go`)
has no production caller and the only reach is `eastereggs_test.go`'s golden.

This is consistent with two recorded omissions rather than an oversight: D236
drops upstream's `telemetry` package entirely and states that `ai`'s optional
`telemetryContext` is never set, and the analytics opt-in exists only to feed
that package. Implementing the dialog without an analytics sink would add a
first-run theme prompt the port has no code to honour.

What a future change needs, if the port ever gains an analytics sink:

1. The settings half already exists: `SetEnableAnalytics` writes
   `enableAnalytics` and, on first opt-in, `trackingId`
   (`coding/settingsmanager.go`), matching `settings-manager.ts`.
2. Port `shouldRunFirstTimeSetup`'s settings-file check and
   `showFirstTimeSetup`'s own startup TUI (the pattern `ShowStartupSelector`
   already uses), then call it from `cmd/main.go` before the app's TUI.
3. The component's `DetectedTheme` option and the "Detected system appearance:"
   line come from the legacy commit the golden pins
   (`d1230ea2`, `coding/interactive/testdata/golden-reference.mjs`); the pin
   (`faac5858`) replaced them with a System/Dark/Light list. Regenerate the
   golden against the legacy commit if the options change, or the check fails.

## One unreproduced `./coding` failure (2026-11-22)

One `go test -count=1 ./coding ./coding/interactive` run reported `FAIL
github.com/dat267/pier/coding 20.861s` while `./coding/interactive` passed. The
failure was not captured with its test name, and thirteen later runs of the same
packages, the same pair, and a `-race` subset all passed. It recurred once more the
same day, again with `./coding/interactive` green and again without the test name
(25 more runs passed, including five `-shuffle=on`).

The next occurrence needs the failing test named, so run without a filter and
keep the whole `--- FAIL:` block:

```sh
go test -count=1 ./coding 2>&1 | grep -B2 -A15 -- '--- FAIL'
```

If it names a test this repository shares with `./coding/interactive`, check
first whether it depends on wall-clock timing or on process-global state
(`leakcheck` and `just test-isolated` both hunt for the second class).
