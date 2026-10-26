// Reference: pi v1.4.0 (4a080252), core/tools/edit.ts (default operations).
// Regenerate from the repository root:
// node --experimental-strip-types coding/testdata/edit-tool/generate.mjs ./pi > coding/testdata/edit-tool/golden.json
import { registerHooks } from "node:module";
import { mkdtemp, writeFile, readFile, rm } from "node:fs/promises";
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
	if (context.parentURL?.endsWith("/core/tools/edit.ts")) {
		if (specifier === "./renderers/edit.ts") return { url: "data:text/javascript,export const editRenderers={};", shortCircuit: true };
		if (specifier === "./tool-definition-wrapper.ts") return { url: "data:text/javascript,export const wrapToolDefinition=(value)=>value;", shortCircuit: true };
	}
	return map[specifier] ? { url: pathToFileURL(resolve(root, map[specifier])).href, shortCircuit: true } : next(specifier, context);
} });
const { createEditToolDefinition } = await import(pathToFileURL(resolve(root, "packages/coding-agent/src/core/tools/edit.ts")));

const dir = await mkdtemp(resolve(tmpdir(), "pier-edit-"));
const seed = {
	"plain.txt": "alpha\nbeta\ngamma\n",
	"crlf.txt": "alpha\r\nbeta\r\ngamma\r\n",
	"bom.txt": "\uFEFFalpha\nbeta\n",
	"multi.txt": "one\ntwo\nthree\nfour\nfive\n",
	"duplicate.txt": "a\na\n",
	"fuzzy.txt": "  alpha   \n  beta\n",
	"no-trailing.txt": "alpha\nbeta",
};
for (const [name, content] of Object.entries(seed)) await writeFile(resolve(dir, name), content);
const cases = [
	{ name: "simple", file: "plain.txt", edits: [{ oldText: "beta", newText: "BETA" }] },
	{ name: "crlf-restored", file: "crlf.txt", edits: [{ oldText: "beta", newText: "BETA" }] },
	{ name: "bom-preserved", file: "bom.txt", edits: [{ oldText: "beta", newText: "BETA" }] },
	{ name: "multiple-edits", file: "multi.txt", edits: [{ oldText: "one", newText: "1" }, { oldText: "five", newText: "5" }] },
	{ name: "fuzzy-indent", file: "fuzzy.txt", edits: [{ oldText: "alpha\nbeta", newText: "alpha\nBETA" }] },
	{ name: "append-trailing-newline", file: "no-trailing.txt", edits: [{ oldText: "beta", newText: "beta\n" }] },
	{ name: "duplicate-error", file: "duplicate.txt", edits: [{ oldText: "a", newText: "b" }] },
	{ name: "not-found-error", file: "plain.txt", edits: [{ oldText: "missing", newText: "x" }] },
	{ name: "no-change-error", file: "plain.txt", edits: [{ oldText: "beta", newText: "beta" }] },
	{ name: "empty-old-text", file: "plain.txt", edits: [{ oldText: "", newText: "x" }] },
	{ name: "missing-file", file: "nope.txt", edits: [{ oldText: "a", newText: "b" }] },
];
const tool = createEditToolDefinition(dir);
const fixtures = [];
for (const testCase of cases) {
	// Each case starts from the seed: earlier cases rewrite their files.
	for (const [name, content] of Object.entries(seed)) await writeFile(resolve(dir, name), content);
	let result;
	try {
		const value = await tool.execute("c", { path: testCase.file, edits: testCase.edits });
		result = {
			text: value.content.filter((block) => block.type === "text").map((block) => block.text).join("\n"),
			details: value.details ?? null,
			file: await readFile(resolve(dir, testCase.file), "utf-8"),
		};
	} catch (error) {
		result = { error: error.message };
	}
	fixtures.push({ name: testCase.name, file: testCase.file, edits: testCase.edits, expected: result });
}
console.log(JSON.stringify(fixtures, null, 2));
await rm(dir, { recursive: true, force: true });
