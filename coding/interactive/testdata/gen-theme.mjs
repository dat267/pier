// Reference: pi v1.4.0 (4a080252), theme.ts and system-theme.ts with pier's palette.
// node --experimental-strip-types coding/interactive/testdata/gen-theme.mjs ./pi
import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { here, upstream, themeModule as m, emit } from "./golden-reference.mjs";
const backgrounds = new Set([
	"selectedBg",
	"searchMatchBg",
	"userMessageBg",
	"customMessageBg",
	"toolPendingBg",
	"toolSuccessBg",
	"toolErrorBg",
]);
const palettes = resolve(process.env.PIER_GOLDEN_PALETTES ?? resolve(here, "../themes"));
for (const name of ["dark", "light"]) {
	const json = JSON.parse(readFileSync(resolve(palettes, name + ".json"), "utf8"));
	for (const mode of ["truecolor", "256color"]) {
		const t = m.loadThemeFromPath(resolve(palettes, name + ".json"), mode);
		for (const key of Object.keys(json.colors))
			emit(
				`theme ${name} ${mode} ${backgrounds.has(key) ? "bg" : "fg"} ${key}`,
				backgrounds.has(key) ? t.getBgAnsi(key) : t.getFgAnsi(key),
			);
		emit(`theme ${name} ${mode} fg-apply`, t.fg("text", "x") + t.bg("selectedBg", "y"));
	}
}
const dark = m.loadThemeFromPath(resolve(palettes, "dark.json"), "truecolor");
emit(
	"theme styles",
	["bold", "italic", "underline", "inverse", "strikethrough"].map((k) => dark[k]("x")),
);
emit(
	"theme thinking",
	["off", "minimal", "low", "medium", "high", "xhigh", "max", "unknown"].map((k) =>
		dark.getThinkingBorderColor(k)("b"),
	),
);
emit("theme bash", dark.getBashModeBorderColor()("b"));
emit("theme colorMode", ["truecolor", "256color"]);
const { terminalAppearance } = await upstream("coding-agent/src/modes/interactive/theme/system-theme.ts");
emit(
	"detect rgb",
	[0, 255, 128, 200, 80, 120].map((v) => terminalAppearance({ r: v, g: v, b: v })),
);
// Preserve the legacy detection projection while calling the pinned detector.
emit(
	"detect env",
	[
		{},
		{ COLORFGBG: "15;0" },
		{ COLORFGBG: "0;15" },
		{ COLORFGBG: "0;7" },
		{ COLORFGBG: "not;a;number" },
		{ COLORFGBG: "0;300" },
	].map((env) => {
		const theme = m.detectColorFgBgTheme(env);
		return theme
			? {
					theme,
					source: "COLORFGBG",
					detail: `background color index ${env.COLORFGBG.split(";").at(-1)}`,
					confidence: "high",
				}
			: { theme: "dark", source: "fallback", detail: "no terminal background hint found", confidence: "low" };
	}),
);
emit(
	"auto setting",
	[undefined, "light/dark", "light/dark/extra", "/dark", "light/", " light / dark ", "plain"].map(
		(s) => m.parseAutoThemeSetting(s) ?? null,
	),
);
emit(
	"resolve setting",
	[
		[undefined, "dark"],
		["light/dark", "light"],
		["light/dark", "dark"],
		["solarized", "dark"],
		["bad/", "dark"],
		["a/b/c", "dark"],
	].map(([s, t]) => m.resolveThemeSetting(s, t) ?? null),
);
emit("available", m.getAvailableThemes());
