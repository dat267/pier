// Reference: pi v1.5.2 (b4afdddf) packages/tui/src/utils.ts autocompleteSeparatorRegex,
// applied the way autocomplete.ts does it: `.test(character)` for one code point.
// tui/autocomplete.go enumerates the (punctuation ∩ CJK-script) part by hand
// because Go's regexp has no Script_Extensions class, so this generator records
// the reference's answer for every code point. It reads the regex out of the
// pinned source instead of retyping it, and prints the matches as ranges.
// Run from the repository root:
//   node tui/testdata/autocomplete-separator/generate.mjs ./pi > tui/testdata/autocomplete-separator/golden.json
import { readFileSync } from "node:fs";
import { join, resolve } from "node:path";

const root = resolve(process.argv[2] ?? "./pi");
const source = readFileSync(join(root, "packages/tui/src/utils.ts"), "utf8");
const breakMatch = source.match(/export const cjkBreakRegex =\s*\/(.+?)\/u;/);
if (!breakMatch) throw new Error("cjkBreakRegex not found in utils.ts");
const punctMatch = source.match(/export const cjkPunctuationRegex = new RegExp\(\s*`([\s\S]*?)`,\s*"u",?\s*\);/);
if (!punctMatch) throw new Error("cjkPunctuationRegex not found in utils.ts");
const template = punctMatch[1]
	.replace(/\$\{cjkBreakRegex\.source\}/g, breakMatch[1])
	.replace(/\\\\/g, "\\");
const cjkPunctuation = new RegExp(template, "u");
const separator = new RegExp(`(?:\\s|${cjkPunctuation.source})`, "u");

const hits = [];
for (let codePoint = 0; codePoint <= 0x10ffff; codePoint++) {
	// Lone surrogates have no Go string form and never appear in editor text.
	if (codePoint >= 0xd800 && codePoint <= 0xdfff) continue;
	if (separator.test(String.fromCodePoint(codePoint))) hits.push(codePoint);
}
const ranges = [];
for (const codePoint of hits) {
	const last = ranges[ranges.length - 1];
	if (last && last[1] === codePoint - 1) last[1] = codePoint;
	else ranges.push([codePoint, codePoint]);
}
process.stdout.write(`${JSON.stringify({ count: hits.length, ranges }, null, "\t")}\n`);
