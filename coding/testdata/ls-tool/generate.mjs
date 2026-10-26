// Reference: pi v1.4.0 (4a080252), core/tools/ls.ts (default operations).
// Regenerate from the repository root:
// node --experimental-strip-types coding/testdata/ls-tool/generate.mjs ./pi > coding/testdata/ls-tool/golden.json
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
	};
	if (context.parentURL?.endsWith("/core/tools/ls.ts")) {
		if (specifier === "./renderers/ls.ts") return { url: "data:text/javascript,export const lsRenderers={};", shortCircuit: true };
		if (specifier === "./tool-definition-wrapper.ts") return { url: "data:text/javascript,export const wrapToolDefinition=(value)=>value;", shortCircuit: true };
	}
	return map[specifier] ? { url: pathToFileURL(resolve(root, map[specifier])).href, shortCircuit: true } : next(specifier, context);
} });
const { createLsToolDefinition } = await import(pathToFileURL(resolve(root, "packages/coding-agent/src/core/tools/ls.ts")));

const dir = await mkdtemp(resolve(tmpdir(), "pier-ls-"));
await mkdir(resolve(dir, "tree/sub"), { recursive: true });
for (const name of ["a.txt", "B.txt", "b.txt", "_x", "-y", ".hidden", "Z", "ä", "10", "9", "a b"]) {
	await writeFile(resolve(dir, "tree", name), "x");
}
await writeFile(resolve(dir, "tree/sub/nested.txt"), "x");
await mkdir(resolve(dir, "empty"));
const cases = [
	{ name: "sorted", path: "tree" },
	{ name: "empty-dir", path: "empty" },
	{ name: "nested", path: "tree/sub" },
	{ name: "limit", path: "tree", limit: 3 },
	{ name: "fractional-limit", path: "tree", limit: 2.5 },
	{ name: "missing", path: "nope" },
	{ name: "not-a-directory", path: "tree/a.txt" },
	{ name: "default-cwd", path: undefined },
];
const tool = createLsToolDefinition(dir);
const fixtures = [];
for (const testCase of cases) {
	const args = {};
	if (testCase.path !== undefined) args.path = testCase.path;
	if (testCase.limit !== undefined) args.limit = testCase.limit;
	let result;
	try {
		const value = await tool.execute("c", args);
		result = {
			text: value.content.filter((block) => block.type === "text").map((block) => block.text).join("\n"),
			details: value.details ?? null,
		};
	} catch (error) {
		result = { error: error.message.replaceAll(dir, "<dir>") };
	}
	fixtures.push({ name: testCase.name, path: testCase.path ?? null, limit: testCase.limit ?? null, expected: result });
}
console.log(JSON.stringify(fixtures, null, 2));
await rm(dir, { recursive: true, force: true });
