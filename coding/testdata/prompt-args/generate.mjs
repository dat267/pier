// Reference: pi v1.5.2 (b4afdddf), packages/coding-agent/src/core/prompt-templates.ts
// (`substituteArgs`, `parseCommandArgs`). A prompt template's text is user data;
// substitution decides what reaches the model, so the cases pin the placeholder
// grammar, the slice forms, the defaults, the missing-argument rules and the
// quoting rules of the argument parser.
//
// Regenerate: node --experimental-strip-types coding/testdata/prompt-args/generate.mjs ./pi \
//   > coding/testdata/prompt-args/golden.json
//
// The corpus is deterministic: the hand-written cases cover the grammar and a
// seeded xorshift generator combines the same pieces. Node's output is the
// expected bytes; never hand-edit the golden.
import { resolve } from "node:path";
import { pathToFileURL } from "node:url";

const root = resolve(process.argv[2] ?? "./pi");
const { substituteArgs, parseCommandArgs } = await import(
	pathToFileURL(resolve(root, "packages/coding-agent/src/core/prompt-templates.ts"))
);

const pieces = [
	"$",
	"$$",
	"$0",
	"$1",
	"$2",
	"$3",
	"$10",
	"$12",
	"${1}",
	"${1:-d}",
	"${@:-d}",
	"${ARGUMENTS:-d}",
	"${2:-}",
	"$@",
	"$ARGUMENTS",
	"${@:1}",
	"${@:2}",
	"${@:1:2}",
	"${@:2:1}",
	"${@:0}",
	"${@:99}",
	"${@:1:0}",
	"${@:2:99}",
	"$ARGUMENTS:1",
	"text",
	" ",
	"a b",
	"'q'",
	'"d"',
	"\n",
	"\\$1",
	"é",
	"*",
	"$1$2",
	"$ARGUMENTS$1",
];
const argsets = [
	[],
	["one"],
	["one", "two"],
	["a b", "c"],
	["1", "2", "3", "4", "5", "6", "7", "8", "9", "10", "11", "12"],
	[""],
	["x", ""],
	["pad", "2", "3", "4", "5", "6", "7", "8", "9", "10", "tail"],
];

const substitutions = [];
const emitSub = (template, args) => substitutions.push({ template, args, result: substituteArgs(template, args) });

// Hand-written grammar cases.
const handwritten = [
	["$1", ["one"]],
	["$2", ["one"]],
	["$1 $2", ["one", "two"]],
	["$10", ["1", "2", "3", "4", "5", "6", "7", "8", "9", "ten"]],
	["$ARGUMENTS", ["a b", "c"]],
	["$@", ["a b", "c"]],
	["$0", ["one"]],
	["$$", ["one"]],
	["${1}", ["one"]],
	["${1} and $1", ["one"]],
	["${@:1}", ["one", "two", "three"]],
	["${@:2}", ["one", "two", "three"]],
	["${@:1:2}", ["one", "two", "three"]],
	["${@:2:1}", ["one", "two", "three"]],
	["${@:0}", ["one", "two"]],
	["${@:0:2}", ["one", "two"]],
	["${@:99}", ["one", "two"]],
	["${@:2:99}", ["one", "two", "three"]],
	["${@:1:0}", ["one", "two"]],
	["${@:3:0}", ["one", "two", "three"]],
	["${@:1:2} ${@:2}", ["a b", "c", "d"]],
	["${@:99999999999}", ["one", "two"]],
	["${@:1:99999999999}", ["one", "two"]],
	["${@:18446744073709551616}", ["one"]],
	["${@:99999999999999999999999999:2}", ["one"]],
	["${1:-d}", []],
	["${1:-d}", ["one"]],
	["${1:-d}", [""]],
	["${2:-fallback}", ["one"]],
	["${@:-none}", []],
	["${@:-none}", ["one", "two"]],
	["${ARGUMENTS:-none}", []],
	["${ARGUMENTS:-none}", ["a b", "c"]],
	["${2:-}", ["one"]],
	["${@:-}", []],
	["$", ["one"]],
	["$*", ["one"]],
	["\\$1", ["one"]],
	["$1$2", ["a", "b"]],
	["plain text", []],
	["", []],
	["   ", []],
	["é $1 é", ["ü"]],
	["line\n$1\nline", ["two"]],
	["$1\n$2", ["a", "b"]],
	["$1-$2-$1", ["a", "b"]],
	["$999", ["a"]],
	["$12345678901234567890", ["a"]],
	["$ARGUMENTS$ARGUMENTS", ["x"]],
	["$1:", ["a", "b"]],
	["$1:-1", ["a", "b", "c"]],
];
for (const [template, args] of handwritten) emitSub(template, args);

// Seeded xorshift so the random half is reproducible without a data file.
let state = 0x2545f491;
const next = () => {
	state ^= state << 13;
	state ^= state >>> 17;
	state ^= state << 5;
	return (state >>> 0) / 0x100000000;
};
for (let index = 0; index < 240; index++) {
	const count = 1 + Math.floor(next() * 4);
	let template = "";
	for (let part = 0; part < count; part++) template += pieces[Math.floor(next() * pieces.length)];
	emitSub(template, argsets[Math.floor(next() * argsets.length)]);
}

const argumentParsing = [];
const emitParse = (input) => argumentParsing.push({ input, result: parseCommandArgs(input) });
const handwrittenArgs = [
	"",
	" ",
	"one",
	"one two",
	"  one   two  ",
	"'one two' three",
	'"one two" three',
	"one\\ two",
	"\\'quoted\\'",
	"a'b'c",
	"a\"b\"c",
	"'unterminated",
	'"unterminated',
	"tab\tseparated",
	"line\nbreak",
	"é ü",
	"''",
	'""',
	"a  b",
	"-flag value",
	"one\\",
];
for (const input of handwrittenArgs) emitParse(input);

const argChars = ["a", " ", "'", '"', "\\", "b", "1", "é", "\t", "\n"];
for (let index = 0; index < 160; index++) {
	const length = Math.floor(next() * 10);
	let input = "";
	for (let char = 0; char < length; char++) input += argChars[Math.floor(next() * argChars.length)];
	emitParse(input);
}

process.stdout.write(`${JSON.stringify({ substitutions, argumentParsing }, null, "\t")}\n`);
