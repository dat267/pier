// Reference: pi v1.4.0 (4a080252), packages/tui/src/keys.ts parseKey.
// Regenerate from repository root:
// node tui/testdata/parse-key/generate.mjs ./pi > tui/testdata/parse-key/golden.json
// Executes the pinned parser for every sequence, with the Kitty protocol both
// inactive (legacy) and active.
import { resolve, join } from "node:path";
import { pathToFileURL } from "node:url";

const root = resolve(process.argv[2] ?? "./pi");
const keys = await import(pathToFileURL(join(root, "packages/tui/src/keys.ts")).href);

const sequences = [
	"a", "A", "z", "Z", "0", "9", " ", "\t", "\r", "\n", "\x7f", "\x00", "\x1b",
	"!", "@", "[", "]", "{", "}", "~", "`", "\\", "|", ";", ":", "'", "\"", ",", ".", "/", "?", "-", "_", "=", "+",
	"\x01", "\x02", "\x03", "\x04", "\x05", "\x06", "\x07", "\x08", "\x0b", "\x0c", "\x0e", "\x0f",
	"\x10", "\x11", "\x12", "\x13", "\x14", "\x15", "\x16", "\x17", "\x18", "\x19", "\x1a", "\x1c", "\x1d", "\x1e", "\x1f",
	"\x1b[A", "\x1b[B", "\x1b[C", "\x1b[D", "\x1b[H", "\x1b[F", "\x1b[Z",
	"\x1b[1;5A", "\x1b[1;2A", "\x1b[1;3C", "\x1b[1;9D", "\x1b[5~", "\x1b[6~", "\x1b[2~", "\x1b[3~",
	"\x1b[1~", "\x1b[4~", "\x1b[7~", "\x1b[8~", "\x1b[11~", "\x1b[15~", "\x1b[24~",
	"\x1b[200~", "\x1b[201~",
	"\x1b[97;5u", "\x1b[97;1u", "\x1b[97:65;2u", "\x1b[13u", "\x1b[27u", "\x1b[9u", "\x1b[57358u",
	"\x1b[97;1:3u", "\x1b[97;5:2u", "\x1b[1;2P", "\x1b[15;5~",
	"\x1bOA", "\x1bOH", "\x1bOP", "\x1b[1;2P",
	"\x1ba", "\x1b\x1b", "\x1b[", "\x1b[1;", "\x1b[<0;1;2M", "\x1b[<0;1;2m",
	"\x1b[I", "\x1b[O", "\x1b[M !!",
	"\u00e9", "\u6f22", "\U0001f600", "\u0301",
	"\x1b[27;5;97~", "\x1b[27;1;13~", "\x1b[3;5~",
];

const output = [];
for (const kitty of [false, true]) {
	keys.setKittyProtocolActive(kitty);
	for (const sequence of sequences) {
		const parsed = keys.parseKey(sequence);
		output.push({
			name: `${kitty ? "kitty" : "legacy"}/${JSON.stringify(sequence)}`,
			sequence,
			kitty,
			expected: parsed === undefined ? null : parsed,
		});
	}
}
keys.setKittyProtocolActive(false);
console.log(JSON.stringify(output, null, 2));
