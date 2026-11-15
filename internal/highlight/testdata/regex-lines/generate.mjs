// Reference: pi v1.5.2 (b4afdddf) highlighted with JavaScript RegExp semantics
// (highlight.js langRe compiles with "m"), against pier's JS-line engine in
// internal/highlight/pattern_regex.go. Run from the repository root:
//   node --experimental-strip-types internal/highlight/testdata/regex-lines/generate.mjs > internal/highlight/testdata/regex-lines/golden.json
// Only patterns the port's RE2 front end accepts appear here. Matches are
// recorded as substrings, not offsets, so byte and UTF-16 indexing disagreeing
// over non-ASCII text cannot mask a semantic difference.
const patterns = [
	"^",
	"$",
	"^a",
	"a$",
	"^.",
	".$",
	"^.*$",
	"^.+",
	".",
	"a.b",
	"[^b]",
	"\\w+",
	"\\s",
	"\\S+",
	"\\r",
	"\\n",
	"\\r\\n",
	"a$|^b",
	"(?:^|\\r)\\n?",
	"\\b",
	"\\B",
	"^\\s*$",
	"[^\\n]+",
	"a\\r?$",
	"^(a|b)",
	"日+",
	// Annex B escapes, braces and classes (patternjs.go).
	"\\x41",
	"\\x4",
	"\\x",
	"\\u0041",
	"\\u041",
	"\\u",
	"\\u{2}",
	"a{2}",
	"a{2,}",
	"a{2,3}",
	"}",
	"a{",
	"a{2",
	"a{,2}",
	"(a){2}",
	"[]",
	"[^]",
	"[\\d-z]",
	"[a-\\d]",
	"[\\w-]",
	// Class complements: JavaScript's \\S excludes the whole ECMAScript whitespace
	// set, while Go's class \\S excludes only ASCII (patternclass.go).
	"[\\S]",
	"[^\\S]",
	"[a\\S]",
	"[^a\\S]",
	"[\\S ]",
	"[\\s\\S]",
	"[^\\s\\S]",
	"[\\s]",
	"[^\\s]",
	"[a\\s]",
];
const flags = ["", "i", "s"];
const inputs = [
	"a\r\nb",
	"a\rb",
	"a\nb",
	"a\u2028b",
	"a\u2029b",
	"a\r\n\r\nb",
	"\r\n",
	"abc",
	"",
	"a b",
	"日本語\r\n",
	"A\r\na",
	"\r",
	"\n",
	"a\r\nb\r\nc",
	"A",
	"aa",
	"aA",
	"{}",
	"-",
	"z",
	"1",
	"a{2}",
	"}",
	"u",
	"x",
	"aaa",
	"\u00a0",
	"\u3000",
	"a\u00a0b",
	"\ufeff",
	"\u200b",
];

const cases = [];
for (const pattern of patterns) {
	const re = new RegExp(pattern, "m");
	for (const extra of flags) {
		const flagged = new RegExp(pattern, `m${extra}`);
		for (const input of inputs) {
			const match = flagged.exec(input);
			const all = [...input.matchAll(new RegExp(pattern, `gm${extra}`))].map((m) => ({
				text: m[0],
				start: m.index,
				end: m.index + m[0].length,
			}));
			cases.push({
				pattern,
				flags: extra,
				input,
				match: match ? match[0] : null,
				all,
			});
		}
	}
	if (re.multiline !== true) throw new Error("m expected");
}
process.stdout.write(`${JSON.stringify(cases, null, "\t")}\n`);
