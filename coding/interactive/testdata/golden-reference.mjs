// Shared read-only reference loader for the interactive golden generators.
// pi v1.4.0 (4a080252); no installed workspace shims or reference edits.
import { registerHooks } from "node:module";
import { execFileSync } from "node:child_process";
import { readFileSync, mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { resolve, dirname } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";
export const here = dirname(fileURLToPath(import.meta.url));
export const root = resolve(process.argv[2] ?? "./pi");
if (!execFileSync("git", ["-C", root, "rev-parse", "HEAD"], { encoding: "utf8" }).startsWith("4a080252")) {
	throw new Error("interactive goldens require read-only pi v1.4.0 (4a080252)");
}
const home = mkdtempSync(resolve(tmpdir(), "pier-interactive-golden-"));
process.env.HOME = home;
process.env.PI_CODING_AGENT_DIR = home;
process.env.FORCE_COLOR = "3";
process.env.COLORTERM = "truecolor";
process.env.TERM = "xterm-256color";
process.env.TZ = "Asia/Bangkok";
process.on("exit", () => rmSync(home, { recursive: true, force: true }));
const palettes = resolve(process.env.PIER_GOLDEN_PALETTES ?? resolve(here, "../themes"));
const legacySources = new Map();
registerHooks({
	resolve(specifier, context, next) {
		const packages = {
			"@earendil-works/pi-tui": "tui",
			"@earendil-works/pi-ai": "ai",
			"@earendil-works/pi-agent-core": "agent",
			"@earendil-works/pi-telemetry": "telemetry",
			"@earendil-works/pi-mcp": "mcp",
		};
		if (specifier === "marked-cjk-friendly") return { url: "pier-golden:cjk", shortCircuit: true };
		if (packages[specifier])
			return {
				url: pathToFileURL(resolve(root, `packages/${packages[specifier]}/src/index.ts`)).href,
				shortCircuit: true,
			};
		return next(specifier, context);
	},
	load(url, context, next) {
		if (url === "pier-golden:cjk")
			return {
				format: "module",
				shortCircuit: true,
				source:
					readFileSync(
						resolve(root, "packages/coding-agent/src/core/export-html/vendor/marked-cjk-friendly.min.js"),
						"utf8",
					) + "\nexport default globalThis.markedCjkFriendly;",
			};
		const result = next(url, context);
		if (legacySources.has(url)) return { ...result, source: legacySources.get(url) };
		if (url.endsWith("/modes/interactive/theme/theme.ts")) {
			// Supply pier's data to the unmodified reference algorithms in memory.
			return { ...result, source: String(result.source).replaceAll("getThemesDir()", JSON.stringify(palettes)) };
		}
		return result;
	},
});
export const upstream = (path) => import(pathToFileURL(resolve(root, "packages", path)).href);
export const component = (name) => upstream(`coding-agent/src/modes/interactive/components/${name}.ts`);
// These existing fixtures predate v1.4.0's setup choices and call-fallback
// arguments. Keep their original contracts rather than bless unrelated
// behavior changes in a palette cleanup. Settings also retains its existing menu
// surface and label casing. All rendering primitives/theme math still run from
// v1.4.0. The original port commits cite d1230ea.
export const legacyComponent = (name) => {
	const path = `packages/coding-agent/src/modes/interactive/components/${name}.ts`;
	const original = execFileSync("git", ["-C", root, "show", `d1230ea:${path}`], { encoding: "utf8" });
	const url = pathToFileURL(resolve(root, path)).href;
	if (name === "tool-execution") {
		const fallback = /private createCallFallback\(\): Component \{[\s\S]*?\n\t\}/;
		const match = original.match(fallback);
		const current = readFileSync(resolve(root, path), "utf8");
		if (!match || !fallback.test(current)) throw new Error("call fallback source moved");
		legacySources.set(
			url,
			current.replace(fallback, () => match[0]),
		);
	} else if (name === "settings-selector") {
		const current = readFileSync(resolve(root, path), "utf8");
		// Preserve the existing fixture's menu surface (no session-share setting)
		// and its display casing; do not silently change settings in a color task.
		legacySources.set(
			url,
			current
				.replace(/\n\t\t\t\{\n\t\t\t\tid: "share-destination",[\s\S]*?\n\t\t\t\},/, "")
				.replace('label: "  automatic"', 'label: "  Automatic"')
				.replace("choose automatic to follow", "choose Automatic to follow"),
		);
	} else legacySources.set(url, original);
	return component(name);
};
export const tui = await upstream("tui/src/index.ts");
tui.setCapabilities({ images: null, trueColor: true, hyperlinks: false });
export const themeModule = await upstream("coding-agent/src/modes/interactive/theme/theme.ts");
themeModule.initTheme("dark");
export const theme = themeModule.theme;
export const corpus = (name) => JSON.parse(readFileSync(resolve(here, `${name}_corpus.json`), "utf8"));
export const entries = (value) => Object.entries(value).sort(([a], [b]) => a.localeCompare(b, "en"));
export const emit = (label, value) => console.log(`${label} => ${JSON.stringify(value)}`);
export const ui = { requestRender() {}, terminal: { columns: 80, rows: 40 }, setFocus() {} };
