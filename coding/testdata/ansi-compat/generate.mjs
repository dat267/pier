// Regenerate: node --experimental-strip-types coding/testdata/ansi-compat/generate.mjs ./pi > coding/testdata/ansi-compat/golden.json
//
// The input corpus is pi v1.5.0 (f9a26365) test/ansi-utils.test.ts
// `getCompatibilityInputs`, plus the cases that test asserts directly for issue
// 11377. Each expectation is the pin's own `stripAnsi` and
// `splitIncompleteAnsiSuffix` (utils/ansi.ts) for that input.
import { resolve } from "node:path";
import { pathToFileURL } from "node:url";

const root = resolve(process.argv[2] ?? "./pi");
const { stripAnsi, splitIncompleteAnsiSuffix } = await import(
	pathToFileURL(resolve(root, "packages/coding-agent/src/utils/ansi.ts"))
);

const inputs = [
	"plain",
	"a\x1b[31mred\x1b[0mz",
	"a\x1b]8;;https://example.com\x07link\x1b]8;;\x07z",
	"a\x1b]unterminated",
	"a\x1b]funterminated",
	"a\x1bPabc\x1b\\z",
	"a\x1b^abc\x07z",
	"a\x1b_abc\x9cz",
	"a\x90abc\x9cz",
	"a\x9dabc\x9cz",
	"a\x9b31mred",
	"a\x1b(0x",
	"a\x1b*0x",
	"a\x1b+c",
	"a\x1b/0x",
	"a\x1bcok",
	"a\x1b\\ok",
];
const chars = ["a", "f", "0", "1", ";", ":", "[", "]", "(", ")", "#", "?", "m", "P", "_", "\\", "\x07", "\x1b", "\x9b", "\x9c", "\x90", "\x9d"];
for (const char of chars) {
	inputs.push(`x\x1b${char}y`);
	inputs.push(`x\x9b${char}y`);
	for (let index = 0; index < chars.length; index += 3) inputs.push(`x\x1b${char}${chars[index]}y`);
}
inputs.push(
	"\x1b[<35;59;1Mstep 1",
	"a\x1b[<0;10;20mb",
	"a\x1b[>0;276;0cb",
	"a\x1b[>cb",
	"a\x1b[=5hb",
	"a\x9b<35;59;1Mb",
	"\x1b[?1h\x1b=123 tests passed",
	"\x1b[?1l\x1b>42 files",
	"\x1b<10 items",
	"\x1bcdone",
	"x\x1b[<35;59",
	"x\x1b=",
);

console.log(
	JSON.stringify(
		inputs.map((input) => {
			const split = splitIncompleteAnsiSuffix(input);
			return { input, strip: stripAnsi(input), complete: split.complete, pending: split.pending };
		}),
		null,
		2,
	),
);
