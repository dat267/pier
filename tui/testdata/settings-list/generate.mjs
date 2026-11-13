// Reference: pi v1.5.2 (b4afdddf), packages/tui/src/components/settings-list.ts
// (SettingsList.render, renderMainList). The settings submenus render through
// it: label column alignment capped at 36 cells, the value column, the scroll
// counter, the wrapped description of the selected item and the keybinding hint
// line. Search is disabled in the corpus so both sides render the same items.
//
// Regenerate from the repository root:
//   node --experimental-strip-types tui/testdata/settings-list/generate.mjs ./pi \
//     > tui/testdata/settings-list/golden.json
import { resolve } from "node:path";
import { pathToFileURL } from "node:url";

const root = resolve(process.argv[2] ?? "./pi");
const { SettingsList } = await import(pathToFileURL(resolve(root, "packages/tui/src/components/settings-list.ts")).href);

const identity = (text) => text;
const theme = {
	label: (text) => text,
	value: (text) => text,
	description: identity,
	cursor: "→ ",
	hint: identity,
};

const item = (id, label, currentValue, description) => {
	const entry = { id, label, currentValue };
	if (description !== undefined) entry.description = description;
	return entry;
};
const sets = {
	empty: [],
	one: [item("theme", "Theme", "dark")],
	five: [
		item("theme", "Theme", "dark"),
		item("model", "Model", "claude-sonnet-4-5"),
		item("steering", "Steering mode", "one-at-a-time"),
		item("compaction", "Auto-compaction", "on"),
		item("retry", "Auto-retry", "on"),
	],
	descriptions: [
		item("theme", "Theme", "dark", "The colour theme used by the interface and the exported HTML."),
		item("model", "Model", "stub/m", "The model new turns use. Existing messages keep their model."),
		item("images", "Images", "off", "Short one."),
	],
	long: [
		item("a", "A very long label that exceeds the alignment cap of thirty-six cells for sure", "value"),
		item("b", "Short", "a value that is much longer than the space left for the value column"),
		item("c", "日本語のラベル", "値"),
		item("d", "😀 emoji label", "emoji value 😀"),
	],
	many: Array.from({ length: 12 }, (_, index) => item(`id${index}`, `Setting ${index + 1}`, `value ${index + 1}`)),
};
const widths = [10, 20, 40, 60, 80, 120];

const records = [];
for (const [name, items] of Object.entries(sets)) {
	for (const width of widths) {
		for (const maxVisible of [1, 3, 5]) {
			for (const selected of [0, Math.floor(Math.max(items.length - 1, 0) / 2), Math.max(items.length - 1, 0)]) {
				const list = new SettingsList(items, maxVisible, theme, () => {}, () => {}, { enableSearch: false });
				if (items[selected]) list.selectItem(items[selected].id);
				records.push({ set: name, width, maxVisible, selected, lines: list.render(width) });
			}
		}
	}
}

process.stdout.write(`${JSON.stringify(records, null, "\t")}\n`);
