// Reference: pi v1.4.0 (4a080252), tui/src/utils.ts truncateToWidth.
// Regenerate from the repository root:
// node --experimental-strip-types tui/testdata/truncate/generate.mjs ./pi > tui/testdata/truncate/golden.json
import { resolve } from "node:path";
import { pathToFileURL } from "node:url";

const root = resolve(process.argv[2] ?? "./pi");
const { truncateToWidth } = await import(pathToFileURL(resolve(root, "packages/tui/src/utils.ts")).href);

const link = (url, text) => `\x1b]8;;${url}\x1b\\${text}\x1b]8;;\x1b\\`;
const cases = [
	["", 0, "...", false], ["", 5, "...", false], ["", 5, "...", true], ["abc", 0, "...", false],
	["abc", -3, "...", true], ["abc", 3, "...", false], ["abc", 2, "...", false], ["abc", 5, "...", true],
	["abcdef", 4, "...", false], ["abcdef", 4, "...", true], ["abcdef", 3, "..", true],
	["abcdef", 2, "", false], ["abcdef", 2, "", true], ["abcdef", 1, "…", false],
	["abcdef", 4, "…", false], ["日本語", 4, "...", false], ["日本語", 4, "...", true],
	["日本語", 2, "日", false], ["日本語", 1, "日", false], ["日本語", 2, "", false],
	["😀😀😀", 5, "...", false], ["😀😀😀", 3, "...", false], ["😀😀😀", 2, "..", true],
	["a😀b", 3, "...", false], ["e\u0301e\u0301", 2, "...", false], ["👨‍👩‍👧‍👦x", 4, "+", false],
	["\x1b[31mred text\x1b[0m", 5, "...", false], ["\x1b[31mred text\x1b[0m", 5, "...", true],
	["\x1b[31m\x1b[1mstyled\x1b[0m tail", 4, "...", false],
	["\x1b[4munderline\x1b[24m plain", 6, "...", false],
	["\x1b[38;2;1;2;3mtruecolor\x1b[0m", 6, "...", false],
	[link("https://example.com", "click here"), 6, "...", false],
	[link("https://example.com", "click here"), 12, "...", true],
	[link("https://example.com", "click here"), 3, "", false],
	["tab\there", 4, "...", false], ["\t\t", 1, "...", true], ["a\tb", 2, "", false],
	["line\nbreak", 6, "...", false], ["spaces   end", 7, "...", false],
	["abc", 1, "日本語", false], ["abcdef", 2, "日本語", true],
	["x", 10, "...", true], ["\x1b[0m", 3, "...", true], ["\x1b[31m", 3, "...", true],
	["\x1b[31mabc", 3, "...", false], ["| a | b |", 4, "...", false],
];
const alphabet = ["a", "bb", "ccc", "\x1b[31m", "\x1b[0m", "\x1b[1m", "\x1b[4m", "\x1b]8;;u\x1b\\",
	"\x1b]8;;\x1b\\", " ", "\t", "日", "😀", "e\u0301", "+", "…", "word"];
let seed = 0x51ed270;
const next = () => {
	seed = (Math.imul(seed, 1103515245) + 12345) >>> 0;
	return seed;
};
for (let i = 0; i < 160; i++) {
	let text = "";
	const parts = next() % 7;
	for (let p = 0; p < parts; p++) text += alphabet[next() % alphabet.length];
	const maxWidth = next() % 14;
	const ellipses = ["...", "", "…", "+", ".."];
	const ellipsis = ellipses[next() % ellipses.length];
	const pad = next() % 2 === 0;
	cases.push([text, maxWidth, ellipsis, pad]);
}
const fixtures = cases.map(([text, maxWidth, ellipsis, pad]) => ({
	text, maxWidth, ellipsis, pad, expected: truncateToWidth(text, maxWidth, ellipsis, pad),
}));
console.log(JSON.stringify(fixtures, null, 2));
