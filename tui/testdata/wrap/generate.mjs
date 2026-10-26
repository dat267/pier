// Reference: pi v1.4.0 (4a080252), tui/src/utils.ts wrapTextWithAnsi.
// Regenerate from the repository root:
// node --experimental-strip-types tui/testdata/wrap/generate.mjs ./pi > tui/testdata/wrap/golden.json
import { resolve } from "node:path";
import { pathToFileURL } from "node:url";

const root = resolve(process.argv[2] ?? "./pi");
const { wrapTextWithAnsi } = await import(pathToFileURL(resolve(root, "packages/tui/src/utils.ts")).href);

const cases = [
	["", 10], [" ", 10], ["   ", 2], ["a", 1], ["ab", 1], ["abc def", 3], ["abc def", 4],
	["one two three four", 7], ["one two three four", 20], ["  leading", 5], ["trailing  ", 5],
	["word ", 4], ["a b", 1], ["\n", 5], ["a\nb", 5], ["a\r\nb", 5], ["a\rb", 5], ["\r\n\r\n", 5],
	["line one\nline two with more words", 8], ["\x1b[31mred\x1b[0m text", 4],
	["\x1b[31mred text that wraps\x1b[0m", 6], ["\x1b[1;4mbold underline wraps here\x1b[0m", 5],
	["\x1b[31mline one\nline two\x1b[0m", 6], ["\x1b[31m\x1b[44mbg\x1b[0m and words", 3],
	["\x1b[38;5;196m256-color text\x1b[0m", 5], ["\x1b[38;2;10;20;30mtruecolor wrapped\x1b[0m", 6],
	["日本語のテキスト", 4], ["日本語 の テキスト", 5], ["😀😀😀", 2], ["a😀b😀c", 3],
	["👍🏽 thumbs", 4], ["e\u0301 combine", 3], ["a\tb", 2], ["\tindented", 4],
	["supercalifragilisticexpialidocious", 5], ["a very long word here", 6],
	["\x1b[31m\x1b[0m", 5], ["\x1b[31m", 5], ["text\x1b[31m", 5], ["\x1b[31mred\nplain", 8],
	["| a | b |", 3], ["**bold** and `code`", 5], ["a  b   c", 3], ["a  b   c", 5],
];
const alphabet = ["a", "bb", "ccc", "\x1b[31m", "\x1b[0m", "\x1b[1m", "\x1b[4m", " ", "  ", "\n", "\r\n",
	"日", "本", "😀", "e\u0301", "\t", "word", "longwordhere", "|", "-"];
let seed = 0x2f6e2b1;
const next = () => {
	seed = (Math.imul(seed, 1103515245) + 12345) >>> 0;
	return seed;
};
for (let i = 0; i < 160; i++) {
	let text = "";
	const parts = next() % 7;
	for (let p = 0; p < parts; p++) text += alphabet[next() % alphabet.length];
	const width = 1 + (next() % 12);
	cases.push([text, width]);
}
const fixtures = cases.map(([text, width]) => ({ text, width, expected: wrapTextWithAnsi(text, width) }));
console.log(JSON.stringify(fixtures, null, 2));
