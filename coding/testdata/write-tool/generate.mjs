// Reference: pi v1.4.0 (4a080252), core/tools/write.ts (default operations).
// Regenerate from the repository root:
// node --experimental-strip-types coding/testdata/write-tool/generate.mjs ./pi > coding/testdata/write-tool/golden.json
import { registerHooks } from "node:module";
import { mkdtemp, mkdir, writeFile, readFile, stat, rm } from "node:fs/promises";
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
	if (context.parentURL?.endsWith("/core/tools/write.ts")) {
		if (specifier === "./renderers/write.ts") return { url: "data:text/javascript,export const writeRenderers={};", shortCircuit: true };
		if (specifier === "./tool-definition-wrapper.ts") return { url: "data:text/javascript,export const wrapToolDefinition=(value)=>value;", shortCircuit: true };
	}
	return map[specifier] ? { url: pathToFileURL(resolve(root, map[specifier])).href, shortCircuit: true } : next(specifier, context);
} });
const { createWriteToolDefinition } = await import(pathToFileURL(resolve(root, "packages/coding-agent/src/core/tools/write.ts")));

const dir = await mkdtemp(resolve(tmpdir(), "pier-write-"));
await writeFile(resolve(dir, "existing.txt"), "old\n");
await mkdir(resolve(dir, "adir"));
const cases = [
	{ name: "create", path: "new.txt", content: "hello\n" },
	{ name: "overwrite", path: "existing.txt", content: "new\n" },
	{ name: "nested-create", path: "deep/er/file.txt", content: "nested\n" },
	{ name: "empty-content", path: "empty.txt", content: "" },
	{ name: "crlf-content", path: "crlf.txt", content: "a\r\nb\r\n" },
	{ name: "no-trailing-newline", path: "tail.txt", content: "tail" },
	{ name: "unicode", path: "emoji.txt", content: "😀 ä\n" },
	// A path that is a directory is not part of the corpus: Node reports
	// "EISDIR: illegal operation on a directory, open '<path>'" and the port
	// keeps Go's os error (D279). TestWriteToolDirectoryError pins the port's
	// message.
];
const tool = createWriteToolDefinition(dir);
const fixtures = [];
for (const testCase of cases) {
	for (const name of ["new.txt", "empty.txt", "crlf.txt", "tail.txt", "emoji.txt"]) await rm(resolve(dir, name), { force: true });
	await rm(resolve(dir, "deep"), { recursive: true, force: true });
	await writeFile(resolve(dir, "existing.txt"), "old\n");
	let result;
	try {
		const value = await tool.execute("c", { path: testCase.path, content: testCase.content });
		let written = null;
		try { written = await readFile(resolve(dir, testCase.path), "utf-8"); } catch {}
		result = {
			text: value.content.filter((block) => block.type === "text").map((block) => block.text).join("\n"),
			details: value.details ?? null,
			written,
		};
	} catch (error) {
		result = { error: error.message.replaceAll(dir, "<dir>") };
	}
	fixtures.push({ name: testCase.name, path: testCase.path, content: testCase.content, expected: result });
}
console.log(JSON.stringify(fixtures, null, 2));
await rm(dir, { recursive: true, force: true });
