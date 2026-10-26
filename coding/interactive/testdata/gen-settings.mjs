// Reference: pi v1.4.0 (4a080252), settings-selector.ts.
import { component, legacyComponent, corpus, entries, emit, themeModule } from "./golden-reference.mjs";
const { SettingsSelectorComponent } = await legacyComponent("settings-selector");
const base = {
	autoCompact: true,
	defaultModel: "anthropic/claude-sonnet-4-5",
	currentModel: { id: "claude-sonnet-4-5", provider: "anthropic", reasoning: true },
	availableDefaultModels: [
		{ id: "gpt-5.2", provider: "openai", reasoning: true },
		{ id: "claude-sonnet-4-5", provider: "anthropic", reasoning: true },
	],
	showImages: true,
	imageWidthCells: 80,
	autoResizeImages: true,
	blockImages: false,
	enableSkillCommands: true,
	steeringMode: "one-at-a-time",
	followUpMode: "all",
	transport: "auto",
	httpIdleTimeoutMs: 30000,
	cacheWarmingMode: "off",
	thinkingLevel: "medium",
	availableThinkingLevels: ["off", "minimal", "low", "medium", "high"],
	modelThinkingLevels: { "anthropic/claude-sonnet-4-5": "high" },
	currentTheme: "dark",
	terminalTheme: "dark",
	availableThemes: themeModule.getAvailableThemes(),
	hideThinkingBlock: false,
	mermaidRenderingMode: "final",
	showCacheMissNotices: true,
	collapseChangelog: true,
	enableInstallTelemetry: false,
	doubleEscapeAction: "tree",
	treeFilterMode: "default",
	showHardwareCursor: false,
	editorPaddingX: 1,
	outputPad: 1,
	autocompleteMaxVisible: 7,
	quietStartup: false,
	defaultProjectTrust: "ask",
	clearOnShrink: true,
	showTerminalProgress: false,
	tuiMode: "regular",
	fullscreenExitOutput: "transcript",
	fullscreenScrollbar: "auto",
	fullscreenCopyOnSelect: true,
	fullscreenWheelScrollLines: "auto",
	warnings: {},
};
for (const [label, s] of entries(corpus("settings").cases)) {
	const events = [];
	const callbacks = new Proxy(
		{},
		{ get: (_, key) => (value) => events.push(`${key}:${JSON.stringify(value ?? null)}`) },
	);
	const v = new SettingsSelectorComponent({ ...base, ...s.config }, callbacks);
	for (const input of s.inputs ?? []) v.getSettingsList().handleInput(input);
	emit(`x ${label}@${s.width}`, v.render(s.width));
	emit(`x ${label}-events`, events);
}
