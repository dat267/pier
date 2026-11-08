// Regenerate from the repository root:
// node coding/testdata/session-roundtrip/generate.mjs ./pi > coding/testdata/session-roundtrip/golden.json
// Reference: pi v1.5.0 (f9a26365), packages/coding-agent/src/core/session-manager.ts
// (SessionManager appends and JSON.stringify). Drives the pinned class in a
// temp agent directory, one of every entry type, then records the bytes it
// wrote. Never uses an installed bundle.
//
// Pierre reads a session written by the reference, then rewrites the file
// after a branch edit or a compaction. That rewrite must reproduce the
// reference's bytes for every entry type, or the two tools diverge on disk.
import { registerHooks } from "node:module";
import { resolve, join } from "node:path";
import { pathToFileURL } from "node:url";
import { mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";

// The fixture has to regenerate byte for byte: the pinned SessionManager
// stamps new Date() and draws ids from randomUUID()/uuidv7(), so freeze the
// clock and the random source.
const FIXED_NOW = Date.parse("2026-01-01T00:00:00.000Z");
const RealDate = Date;
globalThis.Date = class extends RealDate {
	constructor(...args) {
		super(...(args.length === 0 ? [FIXED_NOW] : args));
	}
	static now() {
		return FIXED_NOW;
	}
};
let randomSeed = 0x2f6b;
Object.defineProperty(globalThis.crypto, "getRandomValues", {
	configurable: true,
	value(array) {
		for (let index = 0; index < array.length; index++) {
			randomSeed = (randomSeed * 1103515245 + 12345) & 0x7fffffff;
			array[index] = randomSeed & 0xff;
		}
		return array;
	},
});
const shimURL = pathToFileURL(resolve(import.meta.dirname, "crypto-shim.mjs")).href;
registerHooks({
	resolve(specifier, context, next) {
		if ((specifier === "crypto" || specifier === "node:crypto") && context.parentURL !== shimURL) {
			return { url: shimURL, shortCircuit: true };
		}
		return next(specifier, context);
	},
});

const root = resolve(process.argv[2] ?? "./pi");
const { SessionManager, loadEntriesFromFile } = await import(
	pathToFileURL(join(root, "packages/coding-agent/src/core/session-manager.ts")).href,
);

const header = (extra = "") =>
	`{"type":"session","version":3,"id":"01a1ba96-586d-7525-b850-b7525027e8de","timestamp":"2026-11-08T09:56:17.646Z","cwd":"/work","futureFlag":true${extra}}`;

// A session from a newer reference: an unknown header member, an unknown
// member on a known message entry and an entry type this revision does not
// model. loadEntriesFromFile keeps every parsed line, so a rewrite must
// reproduce these bytes exactly; dropping them would lose data on disk.
const forwardCompat = [
	header(),
	`{"type":"message","id":"0ded14bc","parentId":null,"timestamp":"2026-11-08T09:56:17.647Z","futureField":[1,2],"message":{"role":"user","timestamp":1767225601000,"content":[{"type":"text","text":"hi"}]}}`,
	`{"type":"settings_snapshot","id":"5f5f5f5f","parentId":"0ded14bc","timestamp":"2026-11-08T09:56:17.648Z","settings":{"theme":"dark"}}`,
	`{"type":"message","id":"51f7940d","parentId":"5f5f5f5f","timestamp":"2026-11-08T09:56:17.649Z","message":{"role":"user","timestamp":1767225602000,"content":[{"type":"text","text":"again"}]}}`,
];

const agentDir = mkdtempSync(join(tmpdir(), "pier-session-roundtrip-"));
const messages = [
	{ role: "user", timestamp: 1767225601000, content: [{ type: "text", text: "hi" }] },
	{
		role: "assistant",
		timestamp: 1767225602000,
		provider: "anthropic",
		model: "claude-sonnet-4-20250514",
		api: "anthropic-messages",
		stopReason: "toolUse",
		usage: {
			input: 12,
			output: 34,
			cacheRead: 5,
			cacheWrite: 6,
			totalTokens: 57,
			cost: { input: 0.000036, output: 0.00051, cacheRead: 0.0000015, cacheWrite: 0.0000072, total: 0.0005547 },
		},
		content: [
			{ type: "thinking", thinking: "plan", signature: "c2lnbmF0dXJl" },
			{ type: "redactedThinking", data: "cmVkYWN0ZWQ=" },
			{ type: "text", text: "calling the tool" },
			{ type: "toolCall", id: "call_1", name: "read", arguments: { path: "a.txt", offset: 2, nested: [1, { b: null }] } },
		],
	},
	{
		role: "toolResult",
		timestamp: 1767225603000,
		toolCallId: "call_1",
		toolName: "read",
		isError: false,
		content: [{ type: "text", text: "file body" }],
	},
	{ role: "bashExecution", command: "echo hi", output: "hi\n", exitCode: 0, cancelled: false, truncated: false, timestamp: 1767225604000 },
	{ role: "custom", customType: "note", content: "custom body", display: true, timestamp: 1767225605000 },
	{ role: "compactionSummary", summary: "earlier work", tokensBefore: 4096, timestamp: 1767225606000 },
	{ role: "branchSummary", summary: "other branch", fromId: "m0", timestamp: 1767225607000 },
];

let session;
try {
	session = SessionManager.create("/work", agentDir);
	const userID = session.appendMessage(messages[0]);
	const assistantID = session.appendMessage(messages[1]);
	session.appendMessage(messages[2]);
	session.appendMessage(messages[3]);
	session.appendMessage(messages[4]);
	session.appendMessage(messages[5]);
	session.appendMessage(messages[6]);
	session.appendThinkingLevelChange("high");
	session.appendModelChange("openai", "gpt-5");
	session.appendUsage("compaction", "anthropic", "claude-sonnet-4-20250514", {
		input: 1,
		output: 2,
		cacheRead: 3,
		cacheWrite: 4,
		totalTokens: 10,
		cost: { input: 0.1, output: 0.2, cacheRead: 0.3, cacheWrite: 0.4, total: 1 },
	}, "summarized");
	session.appendUsage("unknown-kind", "openai", "gpt-5", { input: 0, output: 0, cacheRead: 0, cacheWrite: 0, totalTokens: 0 });
	session.appendSessionInfo("  a titled session  ");
	session.appendCustomEntry("tool-state", { enabled: true, names: ["a", "b"] });
	session.appendCustomMessageEntry("notice", "visible note", true, { level: "warn" });
	const hiddenID = session.appendCustomMessageEntry("notice", [{ type: "text", text: "hidden" }], false);
	session.appendContextEdit(hiddenID, { content: "replaced" });
	session.appendContextEdit(userID, null);
	session.appendLabelChange(userID, "first");
	session.appendLabelChange(assistantID, undefined);
	session.appendCompaction("compacted", userID, 12345, { strategy: "summary", kept: [1, 2] }, true, {
		input: 7,
		output: 8,
		cacheRead: 9,
		cacheWrite: 10,
		totalTokens: 34,
		cost: { input: 0.01, output: 0.02, cacheRead: 0.03, cacheWrite: 0.04, total: 0.1 },
	});
	const content = readFileSync(session.getSessionFile(), "utf8");

	// Round-trip the forward-compatible file through the pinned loader and the
	// stringify a rewrite performs, to prove the reference keeps the bytes.
	const seeded = `${forwardCompat.join("\n")}\n`;
	const seededPath = join(agentDir, "forward-compat.jsonl");
	writeFileSync(seededPath, seeded);
	const rewritten = loadEntriesFromFile(seededPath).map((entry) => JSON.stringify(entry)).join("\n") + "\n";

	process.stdout.write(`${JSON.stringify({
		cases: [
			{ name: "entry-types", content },
			{ name: "forward-compat", seeded, content: rewritten },
		],
	}, null, "\t")}\n`);
} finally {
	rmSync(agentDir, { recursive: true, force: true });
}
