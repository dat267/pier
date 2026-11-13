// Reference: pi v1.5.2 (b4afdddf), packages/tui/src/components/text.ts and
// components/box.ts (Text.render, Box.render). Panels, tool output and message
// blocks are a box around text, so padding, wrapping width, background fill and
// the interaction between the two paddings are compared here. The background
// function is a marker wrapper so the fill of each line is visible in the
// golden.
//
// Regenerate from the repository root:
//   node --experimental-strip-types tui/testdata/box/generate.mjs ./pi \
//     > tui/testdata/box/golden.json
import { resolve } from "node:path";
import { pathToFileURL } from "node:url";

const root = resolve(process.argv[2] ?? "./pi");
const { Text } = await import(pathToFileURL(resolve(root, "packages/tui/src/components/text.ts")).href);
const { Box } = await import(pathToFileURL(resolve(root, "packages/tui/src/components/box.ts")).href);

const texts = {
	empty: "",
	short: "hello",
	words: "one two three four five six seven eight nine ten",
	longWord: "supercalifragilisticexpialidocious",
	multiline: "first line\nsecond line is longer\nthird",
	trailingSpaces: "trailing   \nspaces   ",
	wide: "日本語のテキスト です",
	emoji: "😀 emoji 😀 text",
	styled: "\x1b[31mred\x1b[0m and \x1b[1mbold\x1b[0m",
	indented: "  two leading spaces",
};
const widths = [5, 10, 20, 40, 80];
const paddings = [
	[0, 0],
	[1, 0],
	[1, 1],
	[2, 1],
	[0, 2],
];
const backgrounds = { none: undefined, marker: (text) => `[${text}]` };

const records = [];
for (const [name, text] of Object.entries(texts)) {
	for (const [px, py] of paddings) {
		for (const [bgName, bg] of Object.entries(backgrounds)) {
			for (const width of widths) {
				const plain = new Text(text, px, py, bg);
				records.push({ component: "text", text: name, paddingX: px, paddingY: py, bg: bgName, width, lines: plain.render(width) });
				const box = new Box(px, py, bg);
				box.addChild(new Text(text, 0, 0));
				records.push({ component: "box", text: name, paddingX: px, paddingY: py, bg: bgName, width, lines: box.render(width) });
			}
		}
	}
}

process.stdout.write(`${JSON.stringify(records, null, "\t")}\n`);
