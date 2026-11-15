// Reference: pi v1.5.2 (b4afdddf) packages/tui/src/utils.ts AnsiCodeTracker, as
// observed through wrapTextWithAnsi (which re-emits the active codes at the
// next line) and getActiveBackgroundAnsi. Run from the repository root:
//   node --experimental-strip-types tui/testdata/ansi-tracker/generate.mjs ./pi > tui/testdata/ansi-tracker/golden.json
import { resolve } from "node:path";
import { pathToFileURL } from "node:url";

const root = resolve(process.argv[2] ?? "./pi");
const { wrapTextWithAnsi, getActiveBackgroundAnsi } = await import(
	pathToFileURL(resolve(root, "packages/tui/src/utils.ts")).href
);

const esc = (params) => `\x1b[${params}m`;
const codes = [
	// Attributes on and off.
	0, "", 1, 2, 3, 4, 5, 7, 8, 9, 21, 22, 23, 24, 25, 27, 28, 29,
	"00", "01",
	// Standard colours and their defaults.
	30, 31, 37, 39, 40, 44, 47, 49, 90, 97, 100, 107,
	// 256-colour and RGB in both slots.
	"38;5;240", "48;5;17", "38;2;1;2;3", "48;2;4;5;6",
	// Combined fields, order effects, and malformed or truncated forms.
	"1;31", "31;44", "38;5;240;1", "1;38;5;240", "31;39", "44;49",
	"38;5", "38;5;", "38;2;1;2", "38;2;1;2;3;4", "48;5", "48;2;9",
	"1;;31", ";", "999", "1;999;4", "031", "1;031", "030",
	// Unknown and non-SGR final bytes stay untracked.
	"1K", "2J",
];
const osc = [
	"\x1b]8;;https://a\x1b\\",
	"\x1b]8;;https://a\x07",
	"\x1b]8;id=1;https://a\x1b\\",
	"\x1b]8;;\x1b\\",
	"\x1b]8;;\x07",
	"\x1b]8;\x07",
];

const cases = [];
for (const params of codes) {
	const code = typeof params === "number" ? esc(params) : esc(params);
	cases.push({ label: `sgr ${params === "" ? "empty" : params}`, input: `${code}x` });
}
for (const code of osc) {
	cases.push({ label: `osc ${JSON.stringify(code)}`, input: `${code}x` });
}
cases.push(
	{ label: "open then text then close", input: "\x1b]8;;https://a\x07x\x1b]8;;\x07" },
	{ label: "sgr then hyperlink", input: "\x1b[1m\x1b]8;;https://a\x07x" },
	{ label: "hyperlink then reset", input: "\x1b]8;;https://a\x07\x1b[31mx\x1b[0m" },
	{ label: "truncated csi then reset", input: "\x1b[1\x1b[0mx" },
);

const results = cases.map(({ label, input }) => ({
	label,
	input,
	// The next line's prefix is the re-established state.
	wrap: wrapTextWithAnsi(`${input}\nnext`, 20),
	background: getActiveBackgroundAnsi(input),
}));
process.stdout.write(`${JSON.stringify(results, null, "\t")}\n`);
