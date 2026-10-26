// Reference: pi v1.4.0 (4a080252), core/tools/read.ts (text path, default ops).
// Regenerate from the repository root:
// node --experimental-strip-types coding/testdata/read-tool/generate.mjs ./pi > coding/testdata/read-tool/golden.json
import { registerHooks } from "node:module";
import { mkdtemp, writeFile, rm, mkdir } from "node:fs/promises";
import { tmpdir } from "node:os";
import { resolve } from "node:path";
import { pathToFileURL } from "node:url";
const root = resolve(process.argv[2] ?? "./pi");
registerHooks({ resolve(specifier, context, next) {
	const map = {
		"@earendil-works/pi-agent-core": "packages/agent/src/index.ts",
		"@earendil-works/pi-ai": "packages/ai/src/index.ts",
		"@earendil-works/pi-tui": "packages/tui/src/index.ts",
		"@earendil-works/pi-telemetry": "packages/telemetry/src/index.ts",
	};
	if (context.parentURL?.endsWith("/core/tools/read.ts")) {
		if (specifier === "./renderers/read.ts") return { url: "data:text/javascript,export const readRenderers={};", shortCircuit: true };
		if (specifier === "./tool-definition-wrapper.ts") return { url: "data:text/javascript,export const wrapToolDefinition=(value)=>value;", shortCircuit: true };
	}
	// The text-only path never decodes images; stub the terminal image module
	// whose Markdown dependency is not installed in this checkout.
	if (specifier === "@earendil-works/pi-tui") {
		return { url: "data:text/javascript,export const getNativeClipboard=()=>null,getCapabilities=()=>({}),setImageTranscoder=()=>{},Image=class{};", shortCircuit: true };
	}
	return map[specifier] ? { url: pathToFileURL(resolve(root, map[specifier])).href, shortCircuit: true } : next(specifier, context);
} });
const { createReadToolDefinition } = await import(pathToFileURL(resolve(root, "packages/coding-agent/src/core/tools/read.ts")));

const dir = await mkdtemp(resolve(tmpdir(), "pier-read-"));
await mkdir(resolve(dir, "sub"), { recursive: true });
await writeFile(resolve(dir, "empty.txt"), "");
await writeFile(resolve(dir, "three.txt"), "one\ntwo\nthree\n");
await writeFile(resolve(dir, "no-newline.txt"), "one\ntwo");
await writeFile(resolve(dir, "blank-lines.txt"), "a\n\n\nb\n");
await writeFile(resolve(dir, "crlf.txt"), "a\r\nb\r\n");
await writeFile(resolve(dir, "bom.txt"), "\uFEFFfirst\nsecond\n");
await writeFile(resolve(dir, "invalid.txt"), Buffer.from([0xff, 0x0a, 0x41, 0x0a]));
await writeFile(resolve(dir, "long-line.txt"), "x".repeat(60000) + "\nsecond\n");
await writeFile(resolve(dir, "many-lines.txt"), Array.from({ length: 2500 }, (_, i) => `line ${i + 1}`).join("\n") + "\n");
await writeFile(resolve(dir, "sub/nested.txt"), "nested\n");
const cases = [
	{ name: "empty", path: "empty.txt" },
	{ name: "three", path: "three.txt" },
	{ name: "no-newline", path: "no-newline.txt" },
	{ name: "blank-lines", path: "blank-lines.txt" },
	{ name: "crlf", path: "crlf.txt" },
	{ name: "bom", path: "bom.txt" },
	{ name: "invalid-utf8", path: "invalid.txt" },
	{ name: "offset-2", path: "three.txt", offset: 2 },
	{ name: "offset-past-end", path: "three.txt", offset: 9 },
	{ name: "offset-zero", path: "three.txt", offset: 0 },
	{ name: "limit-2", path: "three.txt", limit: 2 },
	{ name: "limit-fractional", path: "three.txt", limit: 1.5 },
	{ name: "offset-and-limit", path: "three.txt", offset: 2, limit: 1 },
	{ name: "long-line", path: "long-line.txt" },
	{ name: "many-lines", path: "many-lines.txt" },
	{ name: "nested", path: "sub/nested.txt" },
	// A missing file is not part of the corpus: Node reports libuv's
	// "ENOENT: no such file or directory, access '<path>'" and the port keeps
	// Go's os error text (D279). TestReadToolMissingFileError covers the port's
	// behavior.
];
const tool = createReadToolDefinition(dir);
const fixtures = [];
for (const testCase of cases) {
	const args = { path: testCase.path };
	if (testCase.offset !== undefined) args.offset = testCase.offset;
	if (testCase.limit !== undefined) args.limit = testCase.limit;
	let result;
	try {
		const value = await tool.execute("c", args);
		result = { text: value.content.filter((block) => block.type === "text").map((block) => block.text).join("\n"), details: value.details ?? null };
	} catch (error) {
		result = { error: error.message };
	}
	fixtures.push({ ...testCase, expected: result });
}
console.log(JSON.stringify(fixtures, null, 2));
await rm(dir, { recursive: true, force: true });
