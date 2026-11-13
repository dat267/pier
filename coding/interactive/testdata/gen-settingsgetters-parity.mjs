// Reference: pi v1.5.2 (b4afdddf), core/settings-manager.ts getters: the default
// each setting falls back to, and the value each document produces.
import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { corpus, entries, emit, upstream } from "./golden-reference.mjs";
const { SettingsManager } = await upstream("coding-agent/src/core/settings-manager.ts");
const c = corpus("settingsgetters");
// getExternalEditorCommand falls back to $VISUAL/$EDITOR; pin them empty so the
// fixture records the built-in default instead of the developer's editor.
delete process.env.VISUAL;
delete process.env.EDITOR;
const getters = [
	"getHideThinkingBlock", "getShowCacheMissNotices", "getCollapseChangelog", "getEnableAnalytics",
	"getTrackingId", "getEnableInstallTelemetry", "getEnableSkillCommands", "getNpmCommand", "getShellPath",
	"getShellCommandPrefix", "getDefaultProjectTrust", "getThemeSetting", "getTheme", "getDefaultProvider",
	"getDefaultModel", "getDefaultThinkingLevel", "getSteeringMode", "getFollowUpMode", "getTransport",
	"getCompactionEnabled", "getBranchSummarySkipPrompt", "getRetryEnabled", "getCacheWarmingMode",
	"getExternalEditorCommand", "getShowImages", "getImageWidthCells", "getClearOnShrink",
	"getShowTerminalProgress", "getTuiMode", "getFullscreenExitOutput", "getFullscreenScrollbar",
	"getFullscreenCopyOnSelect", "getFullscreenWheelScrollLines", "getImageAutoResize", "getBlockImages",
	"getDoubleEscapeAction", "getTreeFilterMode", "getShowHardwareCursor", "getEditorPaddingX", "getOutputPad",
	"getAutocompleteMaxVisible", "getCodeBlockIndent", "getMermaidRenderingMode", "getLastChangelogVersion",
	"getSessionDir", "getHttpIdleTimeoutMs",
];
for (const [label, s] of entries(c.cases)) {
	const dir = mkdtempSync(join(tmpdir(), "pier-settings-golden-"));
	try {
		const cwd = join(dir, "project");
		const agentDir = join(dir, "agent");
		mkdirSync(join(cwd, ".pi"), { recursive: true });
		mkdirSync(agentDir, { recursive: true });
		if (s.global) writeFileSync(join(agentDir, "settings.json"), JSON.stringify(s.global));
		if (s.project) writeFileSync(join(cwd, ".pi", "settings.json"), JSON.stringify(s.project));
		const manager = SettingsManager.create(cwd, agentDir, { projectTrusted: s.projectTrusted ?? true });
		// An array in getter order: the reference's object key order is the call
		// order, and a Go map would sort it.
		const values = getters.map((getter) => manager[getter]());
		emit(`x ${label}`, values);
	} finally {
		rmSync(dir, { recursive: true, force: true });
	}
}
