// Reference: pi v1.5.2 (b4afdddf), packages/tui/src/components/select-list.ts
// (SelectList.render, renderItem, getPrimaryColumnWidth, getVisibleRange). Every
// picker renders through it, so the selected prefix, the description column
// threshold (width > 40), the primary-column clamp, the scroll counter and the
// visible window are compared here. Control characters are left out: pier
// sanitizes item text (D342, D451) and the pin does not.
//
// Regenerate from the repository root:
//   node --experimental-strip-types tui/testdata/select-list/generate.mjs ./pi \
//     > tui/testdata/select-list/golden.json
import { resolve } from "node:path";
import { pathToFileURL } from "node:url";

const root = resolve(process.argv[2] ?? "./pi");
const { SelectList } = await import(pathToFileURL(resolve(root, "packages/tui/src/components/select-list.ts")).href);

const identity = (text) => text;
const theme = {
	selectedPrefix: identity,
	selectedText: identity,
	description: identity,
	scrollInfo: identity,
	noMatch: identity,
};

const item = (label, description) => (description === undefined ? { value: label, label } : { value: label, label, description });
const sets = {
	empty: [],
	one: [item("only")],
	five: [item("alpha"), item("beta"), item("gamma"), item("delta"), item("epsilon")],
	descriptions: [
		item("read", "Read a file from disk"),
		item("bash", "Run a shell command"),
		item("edit", "Replace text in a file"),
	],
	mixed: [
		item("short", "d"),
		item("a much longer label than the column", "a long description that has to be cut somewhere"),
		item("日本語のラベル", "説明文"),
		item("😀 emoji", "wide chars"),
		item("", "empty label"),
		item("valueonly", ""),
	],
	many: Array.from({ length: 12 }, (_, index) => item(`item ${index + 1}`, `description ${index + 1}`)),
	wide: [item("日本語日本語日本語日本語", "説明"), item("x", "y")],
};
const layouts = [
	{},
	{ minPrimaryColumnWidth: 10, maxPrimaryColumnWidth: 20 },
	{ maxPrimaryColumnWidth: 8 },
	{ minPrimaryColumnWidth: 40 },
];
const widths = [10, 20, 39, 40, 41, 60, 80];

const records = [];
for (const [name, items] of Object.entries(sets)) {
	for (const [layoutIndex, layout] of layouts.entries()) {
		for (const width of widths) {
			for (const maxVisible of [1, 3, 5, 10]) {
				for (const selected of [0, Math.floor(Math.max(items.length - 1, 0) / 2), Math.max(items.length - 1, 0)]) {
					const list = new SelectList(items, maxVisible, theme, layout);
					list.setSelectedIndex(selected);
					records.push({ set: name, layout: layoutIndex, width, maxVisible, selected, lines: list.render(width) });
				}
			}
		}
	}
}

process.stdout.write(`${JSON.stringify(records, null, "\t")}\n`);
