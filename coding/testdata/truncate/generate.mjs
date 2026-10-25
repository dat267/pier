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
const lineCases = ["", "short", repeat("a", 500), repeat("b", 501), `${repeat("漢", 250)}${repeat("😀", 10)}`, `${repeat("c", 499)}😀`];
for (const line of lineCases) {
	for (const maxChars of [500, 10, 0]) {
		outputs.push({ name: `line/${line.length}/${maxChars}`, kind: "line", line, maxChars, expected: truncate.truncateLine(line, maxChars) });
	}
}
const middleCases = [
	{ content: "", maxBytes: 10 },
	{ content: "short text", maxBytes: 10 },
	{ content: repeat("漢", 20), maxBytes: 15 },
	{ content: `${lines(100)}\n`, maxBytes: 100 },
	{ content: repeat("😀", 10), maxBytes: 9 },
];
for (const { content, maxBytes } of middleCases) {
	outputs.push({ name: `middle/${content.length}/${maxBytes}`, kind: "middle", content, maxBytes, expected: truncate.truncateMiddle(content, maxBytes) });
}
const sizes = [0, 1, 1023, 1024, 1025, 51200, 1048575, 1048576, 1572864];
for (const bytes of sizes) {
	outputs.push({ name: `size/${bytes}`, kind: "size", bytes, expected: truncate.formatSize(bytes) });
}
console.log(JSON.stringify(outputs, null, 2));
