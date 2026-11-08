// Reference: pi v1.5.0 (f9a26365), packages/ai/src/utils/json-parse.ts
// `parseToolCallArguments`, whose `arguments` object is what a finished tool
// call carries into the session and back to the provider.
// Regenerate: node --experimental-strip-types ai/testdata/tool-arguments/generate.mjs ./pi > ai/testdata/tool-arguments/golden.json
//
// The corpus is deterministic. The hand-written cases pin the rules that have
// bitten before: key order (including array-index keys first), JavaScript
// number formatting, -0 and overflow to null, duplicate keys keeping their
// first position, `__proto__` as an own member, unicode and surrogate escapes,
// the repair-eligible escapes, and the non-object guard. The seeded xorshift
// cases cover numbers, nesting and escapes in combination. Node's output is the
// expected bytes; never hand-edit the golden. `error` records only whether the
// reference reported an argumentsError: D221 says its text is Go's, not V8's.
import { resolve } from "node:path";
import { pathToFileURL } from "node:url";

const root = resolve(process.argv[2] ?? "./pi");
const { parseToolCallArguments } = await import(
	pathToFileURL(resolve(root, "packages/ai/src/utils/json-parse.ts"))
);

const handWritten = [
	'{"z":1,"a":2}',
	'{"path":"a","content":"b","mode":"w"}',
	'{"outer":{"z":1,"a":2},"list":[{"b":1,"a":2}]}',
	'{ "n": 1.0 , "m": 1e2 }',
	'{"z":0,"10":10,"2":2,"a":1}',
	'{"4294967295":1,"4294967294":2,"01":3,"0":4,"-0":5}',
	'{"n":-0,"nested":{"9":9,"3":3,"z":0}}',
	'{"n":1e400,"negative":-1e400}',
	'{"a":1,"b":2,"a":3}',
	'{"__proto__":{"polluted":true},"b":{"__proto__":"x"}}',
	'{"text":"caf\\u00e9 \\url"}',
	'{"path":"a.tex","content":"\\usepackage{x}","mode":"w"}',
	'{"emoji":"\\ud83d\\ude00","raw":"é😀"}',
	'{"s":"a\\tb\\nc\\"d\\\\e"}',
	'{"big":9007199254740993,"neg":-9007199254740993}',
	'{"e21":1e21,"e20":1e20,"e-7":1e-7,"e-6":1e-6}',
	'{"tiny":5e-324,"huge":1.7976931348623157e308}',
	'{"frac":0.1,"sum":0.30000000000000004,"third":0.3333333333333333}',
	'{"arr":[1,[2,[3,{"four":4}]]]}',
	'{"f":1.5,"g":-1.5,"h":2.50,"i":1E+2}',
	'{"nested":{"a":{"b":{"c":{"d":[1,2,3]}}}}}',
	'{"x":"\\u0000\\u001f"}',
	'{"unpaired":"\\ud83d"}',
	'',
	'   ',
	'{',
	'{"a":}',
	'[1,2]',
	'"text"',
	'null',
	'not json',
	'{"a":1} trailing',
	'{"a":1,}',
	'{"a":[1,2,]}',
	'{"a":1 "b":2}',
];

const atoms = [
	"1", "-0", "0.1", "1e2", "1E-2", "2.50", "-1.5", "1e400", "9007199254740993",
	"true", "false", "null", '"s"', "[]", "{}", '{"z":1,"0":2}', "[1,2]", '"\\u00e9"',
	'"a\\tb"', '"\\ud83d"', '{"b":1,"a":2}', '"é😀"',
];
const keys = ['"a"', '"b"', '"0"', '"2"', '"10"', '"__proto__"', '"01"', '"é"', '"z"'];

let state = 0x1234567;
function next() {
	state ^= state << 13;
	state >>>= 0;
	state ^= state >>> 17;
	state ^= state << 5;
	state >>>= 0;
	return state;
}

const documents = [...handWritten];
for (let index = 0; index < 400; index++) {
	const members = 1 + (next() % 4);
	const parts = [];
	for (let member = 0; member < members; member++) {
		parts.push(`${keys[next() % keys.length]}:${atoms[next() % atoms.length]}`);
	}
	documents.push(`{${parts.join(",")}}`);
}

const golden = documents.map((input) => {
	const parsed = parseToolCallArguments(input);
	return {
		input,
		arguments: JSON.stringify(parsed.arguments),
		error: parsed.argumentsError !== undefined,
	};
});
process.stdout.write(`${JSON.stringify(golden, null, "\t")}\n`);
