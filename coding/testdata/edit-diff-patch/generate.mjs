// Reference: pi v1.4.0 (4a080252), core/tools/edit-diff.ts
// generateUnifiedPatch and generateDiffString, which delegate to jsdiff 8.0.4.
// Regenerate from repository root:
// node coding/testdata/edit-diff-patch/generate.mjs ./pi > coding/testdata/edit-diff-patch/golden.json
// Edit-diff.ts has no bundler-only imports, so the pinned module runs directly.
import { resolve, join } from "node:path";
import { pathToFileURL } from "node:url";

const root = resolve(process.argv[2] ?? "./pi");
const { generateUnifiedPatch, generateDiffString } = await import(
	pathToFileURL(join(root, "packages/coding-agent/src/core/tools/edit-diff.ts")).href,
);

const text = (lines, trailingNewline) => lines.join("\n") + (trailingNewline ? "\n" : "");
const repeated = (count, prefix = "line") => Array.from({ length: count }, (_, index) => `${prefix}${index}`);

const pairs = [];
const add = (name, oldContent, newContent) => pairs.push({ name, oldContent, newContent });

add("identical", "same\n", "same\n");
add("newline-only", "same", "same\n");
add("newline-only-reverse", "same\n", "same");
add("from-empty", "", "new\n");
add("to-empty", "old\n", "");
add("both-empty", "", "");
add("both-empty-different-newline", "", "\n");
add("prepend", "b\nc\n", "a\nb\nc\n");
add("append", "a\nb\n", "a\nb\nc\n");
add("delete-middle", "a\nb\nc\n", "a\nc\n");
add("change-first", "a\nb\nc\n", "x\nb\nc\n");
add("change-last", "a\nb\nc\n", "a\nb\nx\n");
add("all-change", "a\nb\nc\n", "x\ny\nz\n");
add("duplicate-lines", "a\na\na\n", "a\n");
add("duplicate-insert", "a\n", "a\na\na\n");
add("interleaved", "a\nb\na\nb\n", "b\na\nb\na\n");
add("crlf", "a\r\nb\r\n", "a\r\nc\r\n");
add("multibyte", "漢\n字\n", "漢\n新\n");
add("astral", "😀\n😁\n", "😀\n😂\n");
add("blank-lines", "a\n\n\nb\n", "a\n\nb\n");
add("long-context", text(repeated(12), true), text([...repeated(5), "changed", ...repeated(6, "tail")], true));
add("two-hunks", text(repeated(20), true), text([...repeated(2), "two", ...repeated(3, "mid"), "nine", ...repeated(14, "tail")], true));
add("no-final-newline-change", "a\nb\nc", "a\nb\nd");
add("only-final-newline-differs", "a\nb\nc\n", "a\nb\nc");
add("trailing-blank", "a\nb\n\n", "a\nb\n");
add("whitespace-only", "a\n \nb\n", "a\n  \nb\n");

const output = pairs.map((pair) => {
	const patch = generateUnifiedPatch("f.txt", pair.oldContent, pair.newContent, 4);
	const display = generateDiffString(pair.oldContent, pair.newContent, 4);
	return {
		...pair,
		expectedPatch: patch,
		expectedDiff: display.diff,
		expectedFirstChangedLine: display.firstChangedLine ?? -1,
	};
});
console.log(JSON.stringify(output, null, 2));
