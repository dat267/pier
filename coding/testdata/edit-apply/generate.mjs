// Reference: pi v1.4.0 (4a080252), core/tools/edit-diff.ts
// applyEditsToNormalizedContent.
// Regenerate from repository root:
// node coding/testdata/edit-apply/generate.mjs ./pi > coding/testdata/edit-apply/golden.json
// The module is pure apart from its jsdiff dependency, so it runs directly.
import { resolve, join } from "node:path";
import { pathToFileURL } from "node:url";

const root = resolve(process.argv[2] ?? "./pi");
const { applyEditsToNormalizedContent } = await import(
	pathToFileURL(join(root, "packages/coding-agent/src/core/tools/edit-diff.ts")).href,
);

const edit = (oldText, newText) => ({ oldText, newText });
const cases = [
	{ name: "exact-single", content: "alpha\nbeta\ngamma\n", edits: [edit("beta", "BETA")] },
	{ name: "exact-multiline", content: "one\ntwo\nthree\nfour\n", edits: [edit("one\ntwo", "1\n2")] },
	{ name: "append-at-end", content: "only", edits: [edit("only", "only\nadded")] },
	{ name: "final-newline-added", content: "same", edits: [edit("same", "same\n")] },
	{ name: "final-newline-removed", content: "same\n", edits: [edit("same\n", "same")] },
	{ name: "two-disjoint", content: "a\nb\nc\nd\ne\n", edits: [edit("a", "A"), edit("e", "E")] },
	{ name: "two-adjacent", content: "a\nb\n", edits: [edit("a", "A"), edit("b", "B")] },
	{ name: "not-found", content: "alpha\n", edits: [edit("missing", "x")] },
	{ name: "not-found-multi", content: "alpha\n", edits: [edit("alpha", "x"), edit("missing", "y")] },
	{ name: "duplicate", content: "a\na\n", edits: [edit("a", "b")] },
	{ name: "duplicate-multi", content: "a\na\nz\n", edits: [edit("a", "b"), edit("z", "y")] },
	{ name: "empty-old-text", content: "alpha\n", edits: [edit("", "x")] },
	{ name: "no-change", content: "alpha\n", edits: [edit("alpha", "alpha")] },
	{ name: "no-change-multi", content: "alpha\nbeta\n", edits: [edit("alpha", "alpha"), edit("beta", "beta")] },
	{ name: "overlap", content: "alpha beta gamma\n", edits: [edit("alpha beta", "X"), edit("beta gamma", "Y")] },
	{ name: "nested", content: "alpha\n", edits: [edit("alpha", "alPHA"), edit("alpha", "ALPHA")] },
	{ name: "fuzzy-trailing-space", content: "alpha   \nbeta\n", edits: [edit("alpha\nbeta", "alpha\nBETA")] },
	{ name: "fuzzy-indent", content: "  alpha\n  beta\n", edits: [edit("\talpha\n\tbeta", "\talpha\n\tBETA")] },
	{ name: "fuzzy-crlf", content: "alpha\r\nbeta\r\n", edits: [edit("alpha beta", "alpha BETA")] },
	{ name: "fuzzy-curly-quotes", content: "say \u201chi\u201d now\n", edits: [edit('say "hi" now', "say hello now")] },
	{ name: "fuzzy-dashes", content: "a \u2013 b\n", edits: [edit("a - b", "a plus b")] },
	{ name: "fuzzy-nbsp", content: "x\u00a0y\n", edits: [edit("x y", "x z")] },
	{ name: "fuzzy-unchanged-lines", content: "keep   \nchange me  \n", edits: [edit("change me", "CHANGED")] },
	{ name: "unicode", content: "\u6f22\u5b57\n\u65b0\n", edits: [edit("\u6f22\u5b57", "\u6f22")] },
	{ name: "astral", content: "\ud83d\ude00\ud83d\ude01\n", edits: [edit("\ud83d\ude01", "\ud83d\ude02")] },
	{ name: "empty-new-text", content: "a\nb\n", edits: [edit("b\n", "")] },
	{ name: "blank-lines", content: "a\n\n\nb\n", edits: [edit("a\n\n", "a\n")] },
	{ name: "insert-between", content: "a\nc\n", edits: [edit("a\nc", "a\nb\nc")] },
	{ name: "adjacent-allowed", content: "abcdef\n", edits: [edit("abc", "X"), edit("def", "Y")] },
	{ name: "crlf-in-old-text", content: "a\nb\n", edits: [edit("a\r\nb", "a\nb")] },
	{ name: "crlf-in-new-text", content: "a\nb\n", edits: [edit("a", "x\r\ny")] },
	{ name: "combining-marks", content: "cafe\u0301\n", edits: [edit("cafe\u0301", "caf\u00e9")] },
	{ name: "fuzzy-duplicate", content: "x  \nx\n", edits: [edit("x", "y")] },
	{ name: "fuzzy-not-found", content: "alpha   \n", edits: [edit("beta", "y")] },
	{ name: "whole-content", content: "one\ntwo\n", edits: [edit("one\ntwo\n", "three\n")] },
	{ name: "three-occurrences", content: "a\na\na\n", edits: [edit("a", "b")] },
	{ name: "many-edits", content: "a\nb\nc\nd\ne\nf\n", edits: [edit("a", "A"), edit("c", "C"), edit("e", "E")] },
	{ name: "fuzzy-no-change", content: "same  \n", edits: [edit("same", "same")] },
];

// A seeded sweep for line shapes the hand cases miss: trailing whitespace,
// tabs, duplicated lines and absent text over 1-3 edits per case.
let state = 0x2545f491;
const nextRandom = () => {
	state ^= state << 13;
	state ^= state >>> 17;
	state ^= state << 5;
	state >>>= 0;
	return state / 0x100000000;
};
const tokens = ["alpha", "beta", "gamma", "  delta", "epsilon  ", "\tzeta", "eta", "", "theta"];
const pick = (list) => list[Math.floor(nextRandom() * list.length)];
const mutate = (text) => {
	switch (Math.floor(nextRandom() * 5)) {
		case 0:
			return text + "  ";
		case 1:
			return text.replace(/^  /, "");
		case 2:
			return text.replace(/"/g, "\u201d");
		case 3:
			return text.toUpperCase();
		default:
			return text + "\t";
	}
};
for (let index = 0; index < 60; index++) {
	const lines = Array.from({ length: 1 + Math.floor(nextRandom() * 5) }, () => pick(tokens));
	const content = lines.join(nextRandom() < 0.2 ? "\r\n" : "\n") + "\n";
	const edits = [];
	for (let count = 1 + Math.floor(nextRandom() * 3), index2 = 0; index2 < count; index2++) {
		const start = Math.floor(nextRandom() * lines.length);
		const length = 1 + Math.floor(nextRandom() * (lines.length - start));
		let oldText = lines.slice(start, start + length).join("\n");
		if (nextRandom() < 0.25) oldText = mutate(oldText);
		if (nextRandom() < 0.1) oldText = "absent-" + index;
		edits.push(edit(oldText, nextRandom() < 0.3 ? "" : pick(tokens)));
	}
	cases.push({ name: "random-" + index, content, edits });
}

const output = cases.map((test) => {
	let result;
	try {
		const applied = applyEditsToNormalizedContent(test.content, test.edits, "f.txt");
		result = { baseContent: applied.baseContent, newContent: applied.newContent, error: null };
	} catch (error) {
		result = { baseContent: null, newContent: null, error: error.message };
	}
	return { ...test, expected: result };
});
console.log(JSON.stringify(output, null, 2));
