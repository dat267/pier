// Reference: pi v1.4.0 (4a080252), core/tools/truncate.ts.
// Regenerate from repository root:
// node coding/testdata/truncate/generate.mjs ./pi > coding/testdata/truncate/golden.json
// Executes the pinned functions directly; every case is deterministic.
import { resolve, join } from "node:path";
import { pathToFileURL } from "node:url";

const root = resolve(process.argv[2] ?? "./pi");
const truncate = await import(pathToFileURL(join(root, "packages/coding-agent/src/core/tools/truncate.ts")).href);

const repeat = (unit, count) => unit.repeat(count);
const lines = (count, prefix = "line") => Array.from({ length: count }, (_, index) => `${prefix}${index}`).join("\n");

const contents = {
	empty: "",
	single: "only one line",
	singleTrailing: "only one line\n",
	exactLines: `${lines(2000)}\n`,
	overLines: `${lines(2001)}\n`,
	overLinesOpen: lines(2001),
	longFirstLine: `${repeat("a", 51200)}\nsecond\n`,
	hugeFirstLine: `${repeat("a", 51201)}\nsecond\n`,
	multibyte: `${repeat("漢", 100)}\n${repeat("😀", 60)}\ntail\n`,
	crlf: "a\r\nb\r\nc\r\n",
	tailPartial: `${lines(10)}\n${repeat("z", 60000)}`,
	singleHugeLine: repeat("q", 60000),
};
const optionSets = {
	default: {},
	tinyLines: { maxLines: 3 },
	tinyBytes: { maxBytes: 40 },
	both: { maxLines: 4, maxBytes: 60 },
};
const outputs = [];
for (const [contentName, content] of Object.entries(contents)) {
	for (const [optionName, options] of Object.entries(optionSets)) {
		const head = truncate.truncateHead(content, options);
		const tail = truncate.truncateTail(content, options);
		outputs.push({
			name: `head/${contentName}/${optionName}`,
			content,
			options,
			expected: head,
		});
		outputs.push({
			name: `tail/${contentName}/${optionName}`,
			content,
			options,
			expected: tail,
		});
	}
}
// A negative budget: the reference's loops simply never run, so both functions
// return a defined empty window, and truncateTail's partial-line branch takes
// the end of the line with a negative length (an empty string). The port
// pre-sized its slice with the negative value and panicked (A468). Content must
// be non-empty here: the reference reads lines[0] before its loop, so an empty
// content throws there.
const negativeOptionSets = {
	negativeLines: { maxLines: -1 },
	negativeBytes: { maxBytes: -1 },
	bothNegative: { maxLines: -5, maxBytes: -5 },
};
for (const [optionName, options] of Object.entries(negativeOptionSets)) {
	for (const [contentName, content] of Object.entries({ single: contents.single, singleHugeLine: contents.singleHugeLine })) {
		outputs.push({ name: `head/${contentName}/${optionName}`, content, options, expected: truncate.truncateHead(content, options) });
		outputs.push({ name: `tail/${contentName}/${optionName}`, content, options, expected: truncate.truncateTail(content, options) });
	}
}
const lineCases = ["", "short", repeat("a", 500), repeat("b", 501), `${repeat("漢", 250)}${repeat("😀", 10)}`, `${repeat("c", 499)}😀`];
for (const line of lineCases) {
	for (const maxChars of [500, 10, 0, -1]) {
		outputs.push({ name: `line/${line.length}/${maxChars}`, kind: "line", line, maxChars, expected: truncate.truncateLine(line, maxChars) });
	}
}
const cap = 20 * 1024;
const mcpCases = [
	{ name: "under", content: repeat("x", cap - 1) },
	{ name: "at-cap", content: repeat("x", cap) },
	{ name: "over-ascii", content: repeat("x", cap + 100) },
	{ name: "over-astral", content: repeat("😀", Math.floor(cap / 4) + 50) },
	{ name: "over-multiline", content: lines(4000) },
];
// Middle truncation moved into the mcp package as a private helper of
// limitMcpContent, which caps at a fixed budget, so the behavior is pinned
// through that public entry point.
const { limitMcpContent } = await import(pathToFileURL(join(root, "packages/mcp/src/index.ts")).href);
const savedPath = "/tmp/full.txt";
const saveOutput = async () => savedPath;
for (const { name, content } of mcpCases) {
	const result = await limitMcpContent([{ type: "text", text: content }], saveOutput);
	outputs.push({
		name: `mcp/${name}`,
		kind: "mcp",
		content,
		savePath: savedPath,
		expected: result.content.map((block) => block.text ?? "").join(""),
	});
}

// Shapes a port can get wrong: several text blocks joined with a newline, the
// image that follows the truncated text, the other blocks that are dropped, and
// a saver that fails.
const image = { type: "image", data: Buffer.from("png-bytes").toString("base64"), mimeType: "image/png" };
const link = { type: "resource_link", uri: "file:///x", name: "x" };
const failingSave = async () => {
	throw new Error("disk full");
};
// Long text is recorded as a repeat spec so the corpus stays readable.
const longText = (char, count) => ({ type: "text", repeat: { char, count } });
const shapeCases = [
	{ name: "two-text-blocks-over", blocks: [longText("a", cap - 10), { type: "text", text: "b".repeat(20) }] },
	{ name: "text-image-over", blocks: [longText("x", cap + 100), image] },
	{ name: "text-link-over", blocks: [longText("x", cap + 100), link] },
	{ name: "image-only", blocks: [image] },
	{ name: "two-text-blocks-under", blocks: [{ type: "text", text: "a" }, { type: "text", text: "b" }] },
	{ name: "save-fails", blocks: [longText("x", cap + 100)], saveError: true },
];
for (const { name, blocks, saveError } of shapeCases) {
	const materialized = blocks.map((block) => (block.repeat ? { type: "text", text: block.repeat.char.repeat(block.repeat.count) } : block));
	const result = await limitMcpContent(materialized, saveError ? failingSave : saveOutput);
	outputs.push({
		name: `mcp/${name}`,
		kind: "mcp-blocks",
		blocks: blocks.map((block) => (block.type === "image" ? { ...block, data: "<image>" } : block)),
		saveError: Boolean(saveError),
		savePath: savedPath,
		fullOutputPath: result.fullOutputPath ?? null,
		expected: JSON.stringify(result.content.map((block) => (block.type === "image" ? { type: "image", mimeType: block.mimeType } : block))),
	});
}
const sizes = [0, 1, 1023, 1024, 1025, 51200, 1048575, 1048576, 1572864];
for (const bytes of sizes) {
	outputs.push({ name: `size/${bytes}`, kind: "size", bytes, expected: truncate.formatSize(bytes) });
}
console.log(JSON.stringify(outputs, null, 2));
