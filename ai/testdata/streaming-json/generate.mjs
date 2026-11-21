// Reference: pi v1.6.0 (faac5858), packages/ai/src/utils/streaming-json.ts
// ToolCallArgumentsStream. For every text below, records the preview after each
// code point and the final arguments, so the port's incremental parser is
// compared against the reference instead of against expectations written by
// hand. The corpus is a list of shapes that break hand-written parsers plus a
// seeded random list; the seed is fixed so regeneration is byte for byte.
//
// The error message of a malformed final text is not recorded: D221 documents
// that the port reports the Go decoder's wording there, so only whether the
// text failed is.
//
// D253: the port's final arguments encode a lone surrogate escape as U+FFFD,
// because a Go string cannot hold an unpaired surrogate, while JSON.stringify
// keeps the escape. The final value is passed through toWellFormed before it is
// recorded, so the corpus pins the port's normalized bytes; a valid pair is
// unaffected and still becomes the character. Previews are recorded as the
// reference writes them: the port keeps a pending surrogate escape in a
// preview, which is the same bytes.
//
// Regenerate from the repository root:
// node --experimental-strip-types ai/testdata/streaming-json/generate.mjs ./pi > ai/testdata/streaming-json/golden.json
import { resolve } from "node:path";
import { pathToFileURL } from "node:url";

const wellFormed = (value) => {
	if (typeof value === "string") return value.toWellFormed();
	if (Array.isArray(value)) return value.map(wellFormed);
	if (value !== null && typeof value === "object") {
		return Object.fromEntries(Object.entries(value).map(([key, item]) => [key.toWellFormed(), wellFormed(item)]));
	}
	return value;
};

const root = resolve(process.argv[2] ?? "./pi");
const { ToolCallArgumentsStream } = await import(
	pathToFileURL(resolve(root, "packages/ai/src/utils/streaming-json.ts")).href
);

const curated = [
	`{"a":1,"b":[1,2,{"c":"x"}],"d":{"e":null}}`,
	`{"path":"/tmp/x","content":"line\\nline","n":-1.5e10}`,
	`[1,2,3,{"k":"v"}]`,
	`{"s":"\\ud83d\\ude00 \\u00e9 \\t \\\\ \\" end"}`,
	`{"nested":{"deep":{"deeper":[true,false,null,{"x":1}]}}}`,
	`{"":"","k":"","arr":[[],{},[[]]]}`,
	`{"big":1e308,"tiny":1e-308,"zero":0,"neg":-0}`,
	`{"unicode":"漢字👨‍👩‍👧","esc":"\\u0000\\u001f"}`,
	`{}`,
	`[]`,
	`{"incomplete":`,
	`{"trailing":1,`,
	`{"dup":1,"dup":2}`,
	`{"__proto__":{"x":1},"y":2}`,
	`{"10":1,"2":2,"b":3,"a":4}`,
	`{"b":1,"10":2,"a":3,"2":4}`,
	`{"01":1,"1":2}`,
	`{" 1":1,"1 ":2}`,
	`{"-1":1,"1e2":2}`,
	`{"a":{"b":{"c":{"d":1}}}}`,
	`{"a":[{"b":1},{"b":2}]}`,
	`{"a":1,"b":2,"c":3,"d":4,"e":5,"f":6,"g":7,"h":8,"i":9,"j":10,"k":11,"l":12,"m":13,"n":14,"o":15,"p":16,"q":17}`,
	`{"k":"\\ud800"}`,
	`{"k":"\\udc00x"}`,
	`{"k":"\\uD83D\\uDE00"}`,
	`{"k":"\\uD83Dz"}`,
	`{"k":"\\q"}`,
	`{"k":"\\u12"}`,
	`{"k":"\\u12G4"}`,
	`{"a":1 "b":2}`,
	`{"a" 1}`,
	`{"a":1,,"b":2}`,
	`{"a":1}}`,
	`{"a":1} trailing`,
	`{"a":tru}`,
	`{"a":truX}`,
	`{"a":nulll}`,
	`{"a":01}`,
	`{"a":+1}`,
	`{"a":.5}`,
	`{"a":1.}`,
	`{"a":-}`,
	`{"a":Infinity}`,
	`{"a":NaN}`,
	`{"a":1e}`,
	`{"a":1e+}`,
	`{"a":1e400}`,
	`{"a":1E400,"b":2}`,
	`{"a":true,"b":false,"c":null}`,
	`  {"a":1}  `,
	`\n\t{"a" : 1 }`,
	`{"a":1,"b":{"c":[1,{"d":"e"}]},"f":[[]]}`,
	`{"key with space":"v","key\\"quote":1}`,
	`{"a":"line\\u0000nul"}`,
	`{"a":"tab\\tnl\\n"}`,
	`{"a":[[[[1]]]]}`,
	`{"a":1,"a":2,"a":3}`,
	`{"a":{"x":1},"a":{"y":2}}`,
	`{"a":[1],"a":[2,3]}`,
	`{"a":{"b":1,"b":2}}`,
	`{"z":1,"0":2,"y":3,"1":4}`,
	`{"a":1},`,
	`{"a":1}{`,
	`{"a":"unterminated`,
	`{"a":"esc at end\\`,
	`{"a":`,
	`{"a"`,
	`{"a`,
	`{`,
	``,
	`{"a":[1,2`,
	`{"a":[1,`,
	`{"a":{"b":`,
	`{"a":{"b"`,
	`{"a":1,"b":2,}`,
	`{"a":1,"b":2,"c":}`,
	`{"a":1,"b":}`,
	`{"a":{}, "b":[]}`,
	`{"a":"\\/","b":"\\\\"}`,
	`{"a":"😀"}`,
	`{"a":"\\ud83d\\ude00\\ud83d\\ude00"}`,
	`{"a":1,"b":[true,false,null,"s",1.5,-2]}`,
	`{"a":-0.0,"b":1e-7,"c":123456789012345678901234567890}`,
	`{"a":1,"0":2}`,
	`{"0":1,"a":2}`,
	`{"a":[0,1,2,3,4,5,6,7,8,9,10,11,12,13,14,15,16,17,18,19]}`,
];

// A seeded generator keeps the random half of the corpus reproducible.
let seed = 12345;
const rand = () => {
	seed = (Math.imul(seed, 1103515245) + 12345) & 0x7fffffff;
	return seed / 0x80000000;
};
const pick = (list) => list[Math.floor(rand() * list.length) % list.length];
const atoms = [
	"{", "}", "[", "]", ":", ",", `"`, "\\", " ", "\t", "\n", "\r",
	"a", "b", "0", "1", "9", "-", "+", ".", "e", "E", "x", "n", "t", "u", "l", "s", "r",
	"true", "false", "null", "NaN", "Infinity",
	"\\u", "\\ud800", "\\udc00", "\\n", "\\t", "\\/", "\\q", "\\uD83D", "\\uDE00",
	"__proto__", `"a"`, `"1"`, `"01"`, `"10"`, `""`, "é", "😀", "漢",
	"0.5", "1e400", "-0", "1e-7", "123456789012345678901234567890",
];
const random = [];
for (let index = 0; index < 250; index++) {
	const parts = [];
	const length = 1 + Math.floor(rand() * 12);
	for (let part = 0; part < length; part++) parts.push(pick(atoms));
	random.push(rand() < 0.7 ? `{${parts.join("")}}` : parts.join(""));
}

const fixtures = [];
for (const text of [...curated, ...random]) {
	const stream = new ToolCallArgumentsStream();
	const previews = [stream.append("")];
	for (const character of text) previews.push(stream.append(character));
	const { arguments: value, argumentsError } = stream.finish();
	fixtures.push({
		text,
		previews: previews.map((preview) => JSON.stringify(preview)),
		final: JSON.stringify(wellFormed(value)),
		hasError: argumentsError !== undefined,
	});
}
console.log(JSON.stringify(fixtures, null, 1));
