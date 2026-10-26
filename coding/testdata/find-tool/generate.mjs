// Reference: pi v1.4.0 (4a080252), core/tools/find.ts with default (fd) operations.
// Regenerate from the repository root:
// node --experimental-strip-types coding/testdata/find-tool/generate.mjs ./pi > coding/testdata/find-tool/golden.json
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
	if (context.parentURL?.endsWith("/core/tools/find.ts")) {
		if (specifier === "./renderers/find.ts") return { url: "data:text/javascript,export const findRenderers={};", shortCircuit: true };
		if (specifier === "./tool-definition-wrapper.ts") return { url: "data:text/javascript,export const wrapToolDefinition=(value)=>value;", shortCircuit: true };
	}
	return map[specifier] ? { url: pathToFileURL(resolve(root, map[specifier])).href, shortCircuit: true } : next(specifier, context);
} });
const { createFindToolDefinition } = await import(pathToFileURL(resolve(root, "packages/coding-agent/src/core/tools/find.ts")));

const dir = await mkdtemp(resolve(tmpdir(), "pier-find-"));
await mkdir(resolve(dir, "src/deep"), { recursive: true });
await mkdir(resolve(dir, "ignored"), { recursive: true });
await mkdir(resolve(dir, ".git"), { recursive: true });
await writeFile(resolve(dir, "readme.md"), "x");
await writeFile(resolve(dir, "src/a.ts"), "x");
await writeFile(resolve(dir, "src/deep/b.spec.ts"), "x");
await writeFile(resolve(dir, "ignored/c.ts"), "x");
await writeFile(resolve(dir, ".gitignore"), "ignored/\n");
await writeFile(resolve(dir, ".hidden.ts"), "x");
const cases = [
	{ name: "basename-glob", pattern: "*.ts" },
	{ name: "path-containing", pattern: "src/**/*.spec.ts" },
	{ name: "hidden", pattern: "*.ts" },
	{ name: "no-match", pattern: "*.nope" },
	{ name: "limit", pattern: "*.ts", limit: 2 },
	{ name: "path-file", pattern: "*.md", path: "readme.md" },
	{ name: "missing-path", pattern: "*.ts", path: "nope" },
];
const tool = createFindToolDefinition(dir);
const fixtures = [];
for (const testCase of cases) {
	const args = { pattern: testCase.pattern };
	if (testCase.path !== undefined) args.path = testCase.path;
	if (testCase.limit !== undefined) args.limit = testCase.limit;
	let result;
	try {
		const value = await tool.execute("c", args);
		result = { text: value.content.filter((block) => block.type === "text").map((block) => block.text).join("\n"), details: value.details ?? null };
	} catch (error) {
		result = { error: error.message.replaceAll(dir, "<dir>") };
	}
	fixtures.push({ name: testCase.name, args, expected: result });
}
console.log(JSON.stringify(fixtures, null, 2));
await rm(dir, { recursive: true, force: true });
