// Reference: pi v1.5.2 (b4afdddf), packages/tui/src/program-status.ts
// (`formatProgramStatus`, `isProgramStatusReply`). The report is an OSC 7501
// sequence the terminal renders as a status indicator, so its pairs, control-
// character replacement, whitespace trimming, byte-limit truncation and base64
// payload all have to match.
//
// Regenerate from the repository root:
//   node --experimental-strip-types tui/testdata/program-status/generate.mjs ./pi \
//     > tui/testdata/program-status/golden.json
import { resolve } from "node:path";
import { pathToFileURL } from "node:url";

const root = resolve(process.argv[2] ?? "./pi");
const { formatProgramStatus, isProgramStatusReply } = await import(
	pathToFileURL(resolve(root, "packages/tui/src/program-status.ts")).href
);

const states = ["idle", "working", "blocked", "done", "error", "clear", "", "bogus"];
const apps = [undefined, "", "pier", "a.b_c-d+e", "x".repeat(32), "x".repeat(33), "bad/name", "Ünï"];
const kinds = [undefined, "", "permission", "question", "auth", "other"];
const messages = [
	undefined, "", " ", "\t\n", "hello", " leading and trailing ", "a\x00b\x07c\x1bd", "tab\there",
	"line\nbreak", "cr\rreturn", "c1\u0085next", "unicode é 日本 😀", "\u00a0nbsp\u00a0", "\ufeffbom",
	"\u3000ideo\u3000", "\u2028line", "x".repeat(2047), "x".repeat(2048), "x".repeat(2049),
	"é".repeat(1024), "é".repeat(1025), "日".repeat(683), "😀".repeat(513), "a" + "é".repeat(1024),
];

// The grid varies state, app and kind; the long messages are exercised once
// each below so the golden does not repeat a 2 KB payload across every cell.
const gridMessages = messages.filter((message) => (message ?? "").length <= 64);

const records = [];
for (const state of states) {
	for (const app of apps) {
		for (const kind of kinds) {
			const message = gridMessages[(state.length + (app?.length ?? 0) + (kind?.length ?? 0)) % gridMessages.length];
			const status = { state };
			if (app !== undefined) status.app = app;
			if (kind !== undefined) status.kind = kind;
			if (message !== undefined) status.message = message;
			records.push({ status, value: formatProgramStatus(status) });
		}
	}
}
for (const message of messages) {
	const status = { state: "working", message };
	records.push({ status, value: formatProgramStatus(status) });
}
for (const sequence of [
	"", "\x1b]7501;?\x1b\\", "\x1b]7501;?\x07", "\x1b]7501;?extra\x1b\\", "\x1b]7501;?\x1b\\x",
	"x\x1b]7501;?\x1b\\", "\x1b]7501;?", "\x1b]7501;\x1b\\", "\x1b]7501;?\n\x1b\\",
]) {
	records.push({ sequence, value: isProgramStatusReply(sequence) });
}

process.stdout.write(`${JSON.stringify(records, null, "\t")}\n`);
