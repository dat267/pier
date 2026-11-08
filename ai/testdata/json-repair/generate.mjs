// Reference: pi v1.5.0 (f9a26365), packages/ai/src/utils/json-parse.ts `repairJson`.
// Regenerate: node --experimental-strip-types ai/testdata/json-repair/generate.mjs ./pi > ai/testdata/json-repair/golden.json
//
// `repairJson` runs on every finished tool call's arguments text before the
// final JSON.parse, so its output decides what a repaired tool call carries.
// The corpus is deterministic: the hand-written cases pin each rule (control
// characters, invalid escapes, \u digits, astral and lone-surrogate escapes,
// unterminated strings) and the seeded xorshift cases cover combinations.
// Node's output is the expected bytes; never hand-edit the golden.
import { resolve } from "node:path";
import { pathToFileURL } from "node:url";

const root = resolve(process.argv[2] ?? "./pi");
const { repairJson } = await import(pathToFileURL(resolve(root, "packages/ai/src/utils/json-parse.ts")));

const handWritten = [
	'{"a":"\\d"}',
	'{"a":"x\ty"}',
	'{"a":"x\u0001y"}',
	'{"a":"\\u12"}',
	'{"a":"\\u12zz"}',
	'{"a":"\\u00e9"}',
	'{"a":"\\uD83D"}',
	'{"a":"\\ud83d\\ude00"}',
	'{"a":"x\\ud83dy"}',
	'{"a":"\\ud83d"}',
	'{"a":"\\udc00\\udc00"}',
	'{"a":"\\',
	'{"a":"\\"}',
	'{"a":"\\\\"}',
	'{"a":"\\/"}',
	'{"a":"\\b\\f\\n\\r\\t"}',
	'{"a":"\\q\\ "}',
	'{"a":"unterminated',
	'{"a":1,"b":{"c":[1,2,"\\x"]}}',
	'[1,2,"\\u00"]',
	'"just a string',
	"",
	"   ",
	'{"é":"😀"}',
	'{"a":"\u0000\u001f"}',
	'{"a":"\\u0000"}',
	'{"a":"\\uZZZZ"}',
	'{"a":"\\uABCD"}',
	'{"a":"é😀 \u0001 \\q"}',
];

const alphabet = [
	'"', "\\", "u", "0", "1", "9", "a", "f", "A", "F", "Z", "/", "b", "n", "t", "r",
	"{", "}", "[", "]", ":", ",", "x", " ", "\t", "\n", "\u0000", "\u0001", "é", "😀",
	"\\u", "\\ud83d", "\\u00e9", "\\d", "\\\\",
];

// xorshift32: the corpus must not depend on Math.random.
let state = 0x9e3779b9;
function next() {
	state ^= state << 13;
	state >>>= 0;
	state ^= state >>> 17;
	state ^= state << 5;
	state >>>= 0;
	return state;
}

const documents = [...handWritten];
for (let index = 0; index < 500; index++) {
	const length = next() % 19;
	let text = "";
	for (let position = 0; position < length; position++) {
		text += alphabet[next() % alphabet.length];
	}
	documents.push(text);
}

const golden = documents.map((input) => ({ input, output: repairJson(input) }));
process.stdout.write(`${JSON.stringify(golden, null, "\t")}\n`);
