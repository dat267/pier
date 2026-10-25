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
];

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
