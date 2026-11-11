// Reference: pi v1.4.0 (4a080252), core/tools/grep.ts (default operations).
// Regenerate from the repository root:
// node --experimental-strip-types coding/testdata/grep-tool/generate.mjs ./pi > coding/testdata/grep-tool/golden.json
import { registerHooks } from "node:module";
import { mkdtemp, mkdir, writeFile, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { resolve } from "node:path";
import { pathToFileURL } from "node:url";
const root = resolve(process.argv[2] ?? "./pi");
registerHooks({ resolve(specifier, context, next) {
	const map = {
		"@earendil-works/pi-agent-core": "packages/agent/src/index.ts",
		"@earendil-works/pi-ai": "packages/ai/src/index.ts",
		"@earendil-works/pi-telemetry": "packages/telemetry/src/index.ts",
		"@earendil-works/pi-schema": "packages/schema/src/index.ts",
		"@earendil-works/pi-schema/formats": "packages/schema/src/formats/index.ts",
	};
	if (context.parentURL?.endsWith("/core/tools/grep.ts")) {
		if (specifier === "./renderers/grep.ts") return { url: "data:text/javascript,export const grepRenderers={};", shortCircuit: true };
		if (specifier === "./tool-definition-wrapper.ts") return { url: "data:text/javascript,export const wrapToolDefinition=(value)=>value;", shortCircuit: true };
	}
	return map[specifier] ? { url: pathToFileURL(resolve(root, map[specifier])).href, shortCircuit: true } : next(specifier, context);
} });
const { createGrepToolDefinition } = await import(pathToFileURL(resolve(root, "packages/coding-agent/src/core/tools/grep.ts")));

const dir = await mkdtemp(resolve(tmpdir(), "pier-grep-"));
await mkdir(resolve(dir, "sub"), { recursive: true });
await writeFile(resolve(dir, "a.txt"), "alpha one\nbeta two\nalpha three\n");
await writeFile(resolve(dir, "b.txt"), "nothing here\n");
await writeFile(resolve(dir, "sub/c.txt"), "deep alpha\n");
await writeFile(resolve(dir, "long.txt"), "x".repeat(600) + "NEEDLE\n");
await writeFile(resolve(dir, "many.txt"), Array.from({ length: 12 }, (_, i) => `match ${i + 1}`).join("\n") + "\n");
const cases = [
	{ name: "simple", pattern: "alpha" },
	{ name: "literal", pattern: "a.b", literal: true, extra: { "c.txt": "a.b\naxb\n" } },
	{ name: "ignore-case", pattern: "ALPHA", ignoreCase: true },
	{ name: "glob", pattern: "alpha", glob: "b.txt" },
	{ name: "path-file", pattern: "alpha", path: "a.txt" },
	{ name: "path-subdir", pattern: "alpha", path: "sub" },
	{ name: "context", pattern: "beta two", context: 1 },
	{ name: "fractional-context", pattern: "beta two", context: 0.5 },
	{ name: "limit", pattern: "match", limit: 3 },
	// Limit selection over several files depends on ripgrep's traversal order;
	// scope it to one file so the chosen matches are deterministic.
	{ name: "fractional-limit", pattern: "alpha", path: "a.txt", limit: 1.5 },
	{ name: "long-line", pattern: "NEEDLE" },
	{ name: "missing-path", pattern: "alpha", path: "nope" },
	// Boundaries of the floored numbers and of the limit-proof rule.
	{ name: "limit-zero", pattern: "match", path: "many.txt", limit: 0 },
	{ name: "limit-negative", pattern: "match", path: "many.txt", limit: -3 },
	{ name: "no-match-with-limit", pattern: "match", path: "a.txt", limit: 1 },
	// many.txt holds twelve matches: at the limit with no further match there is
	// no truncation proof, so no notice; one below it, the extra match proves it.
	// A per-case file, so the directory-wide "match" cases are unchanged.
	{ name: "limit-equals-match-count", pattern: "row", path: "rows.txt", limit: 2, extra: { "rows.txt": "row 1\nrow 2\n" } },
	{ name: "limit-one-below-match-count", pattern: "row", path: "rows.txt", limit: 1, extra: { "rows.txt": "row 1\nrow 2\n" } },
	{ name: "context-fraction-below-one", pattern: "beta two", context: 0.99 },
	{ name: "context-beyond-file", pattern: "beta two", context: 50 },
	{ name: "context-at-first-line", pattern: "alpha one", context: 2 },
	{ name: "limit-with-context", pattern: "alpha", path: "a.txt", limit: 1, context: 1 },
	{ name: "fractional-limit-two-point-nine", pattern: "match", path: "many.txt", limit: 2.9 },
];
const tool = createGrepToolDefinition(dir);
const fixtures = [];
for (const testCase of cases) {
	if (testCase.extra) for (const [name, content] of Object.entries(testCase.extra)) await writeFile(resolve(dir, name), content);
	const args = { pattern: testCase.pattern };
	for (const key of ["path", "glob", "ignoreCase", "literal", "context", "limit"]) if (testCase[key] !== undefined) args[key] = testCase[key];
	let result;
	try {
		const value = await tool.execute("c", args);
		const raw = value.content.filter((block) => block.type === "text").map((block) => block.text).join("\n");
		// ripgrep's traversal order across files is not stable; compare sorted lines.
		const lines = raw.split("\n");
		const footer = lines.findIndex((line) => line.startsWith("[") && line.endsWith("]"));
		const body = footer === -1 ? lines : lines.slice(0, footer).filter((line) => line !== "");
		const notices = footer === -1 ? [] : lines.slice(footer);
		result = { text: [...body.sort(), ...notices].join("\n"), details: value.details ?? null };
	} catch (error) {
		result = { error: error.message.replaceAll(dir, "<dir>") };
	}
	fixtures.push({ name: testCase.name, args, extra: testCase.extra, expected: result });
	if (testCase.extra) for (const name of Object.keys(testCase.extra)) await rm(resolve(dir, name), { force: true });
}
console.log(JSON.stringify(fixtures, null, 2));
await rm(dir, { recursive: true, force: true });
