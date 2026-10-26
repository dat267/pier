// Reference: pi v1.4.0 (4a080252), ai/src/utils/hash.ts shortHash.
// Regenerate from the repository root:
// node --experimental-strip-types ai/testdata/short-hash/generate.mjs ./pi > ai/testdata/short-hash/golden.json
import { resolve } from "node:path";
import { pathToFileURL } from "node:url";
const root = resolve(process.argv[2] ?? "./pi");
const { shortHash } = await import(pathToFileURL(resolve(root, "packages/ai/src/utils/hash.ts")));
const inputs = [
	"", "a", "abc", "call_abcdefghijklmnop", "😀", "😀😀", "a😀b", "ß", "İ", "line\nbreak", "x".repeat(1000),
	"error failed with status 429", "deep/seek:model", "toolcall:0", "toolcall:12", "\u0000", " ", "abcabc",
];
console.log(JSON.stringify(inputs.map((input) => ({ input, expected: shortHash(input) })), null, 2));
