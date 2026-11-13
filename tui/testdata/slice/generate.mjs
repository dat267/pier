// Reference: pi v1.5.2 (b4afdddf), packages/tui/src/utils.ts (`sliceWithWidth`,
// `getOsc8LinkAtColumn`, `getGraphemeCellRange`, `visibleWidth`). Slices feed
// selection, overlay compositing and row clipping, so a boundary rule that
// drifts moves text, styles or one-shot commands to the wrong column.
//
// Regenerate from the repository root:
//   node --experimental-strip-types tui/testdata/slice/generate.mjs ./pi > tui/testdata/slice/golden.json
import { resolve } from "node:path";
import { pathToFileURL } from "node:url";

const root = resolve(process.argv[2] ?? "./pi");
const { sliceWithWidth, getOsc8LinkAtColumn, getGraphemeCellRange, visibleWidth } = await import(
	pathToFileURL(resolve(root, "packages/tui/src/utils.ts")).href
);

// Plain text, wide and combined graphemes, SGR styling, OSC 8 links, tabs, an
// APC cursor marker, a kitty command, and an empty/pre-styled line.
const lines = [
	"abcdef",
	"日本語",
	"a日b",
	"😀x",
	"e\u0301x",
	"\x1b[31mred\x1b[0m text",
	"\x1b]8;;https://a\x1b\\link\x1b]8;;\x1b\\ tail",
	"\x1b]8;;https://a\x1b\\日本\x1b]8;;\x1b\\x",
	"a\tb",
	"\x1b[31m\x1b]8;;u\x1b\\wide 日本\x1b]8;;\x1b\\\x1b[0m",
	"\x1b_Ga=T,f=100,i=7;AAAA\x1b\\",
	"\x1b_apc\x1b\\text",
	"👨‍👩‍👧x",
	"",
	"\x1b[31m",
];

const out = [];
lines.forEach((line, index) => {
	const width = visibleWidth(line);
	out.push({ kind: "width", line: index, width });
	for (let column = 0; column <= width + 2 && column <= 20; column++) {
		out.push({ kind: "link", line: index, column, value: getOsc8LinkAtColumn(line, column) ?? null });
		const range = getGraphemeCellRange(line, column);
		out.push({ kind: "cell", line: index, column, value: range === undefined ? null : [range.start, range.end] });
	}
	for (let start = 0; start <= 4; start++) {
		for (let length = 0; length <= 5; length++) {
			for (const strict of [false, true]) {
				const sliced = sliceWithWidth(line, start, length, strict);
				out.push({ kind: "slice", line: index, start, length, strict, text: sliced.text, width: sliced.width });
			}
		}
	}
});

process.stdout.write(`${JSON.stringify(out, null, "\t")}\n`);
