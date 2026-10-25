// Regenerate from the repository root:
// node coding/testdata/session-load/generate.mjs ./pi > coding/testdata/session-load/golden.json
// Reference: pi v1.4.0 (4a080252), packages/coding-agent/src/core/session-manager.ts
// (loadEntriesFromFile, SessionManager.list). Executes the pinned functions
// over temp session files, never an installed bundle.
import { resolve, join } from "node:path";
import { pathToFileURL } from "node:url";
import { mkdtempSync, writeFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";

const root = resolve(process.argv[2] ?? "./pi");
const { loadEntriesFromFile, SessionManager } = await import(
	pathToFileURL(join(root, "packages/coding-agent/src/core/session-manager.ts")).href,
);

const ts = "2026-01-01T00:00:00.000Z";
const user = JSON.stringify({ type: "message", id: "m1", parentId: null, timestamp: "2026-01-01T00:00:01.000Z",
	message: { role: "user", timestamp: 1767225601000, content: [{ type: "text", text: "hi" }] } });
const header = (idJson) => `{"type":"session","id":${idJson},"timestamp":"${ts}","cwd":"/work"}`;

const cases = {
	"valid-id": [header('"s1"'), user],
	"empty-id": [header('""'), user],
	"missing-id": [`{"type":"session","timestamp":"${ts}","cwd":"/work"}`, user],
	"null-id": [header("null"), user],
	"non-string-id": [header("5"), user],
	"non-session-first": [user, header('"s1"')],
	"malformed-first": ["{not json", header('"s1"'), user],
	"blank-first": ["", header('"s1"'), user],
};

const out = {};
for (const [name, lines] of Object.entries(cases)) {
	const dir = mkdtempSync(join(tmpdir(), "pier-session-load-"));
	try {
		const file = join(dir, "session.jsonl");
		writeFileSync(file, lines.join("\n") + "\n");
		const entries = loadEntriesFromFile(file).map((entry) =>
			entry.type === "session"
				? { kind: "session", type: "session", id: entry.id }
				: { kind: "entry", type: entry.type, id: entry.id },
		);
		// Listing is recorded only for headers whose id is a string: for a
		// missing/null/non-string id the pin lists a session it cannot load
		// (garbage id), while Pier rejects the malformed header in both paths
		// (see DIVERGENCES D278).
		const listed = await SessionManager.list("/work", dir);
		const record = { lines, entries };
		if (["valid-id", "empty-id", "non-session-first", "malformed-first", "blank-first"].includes(name)) {
			record.list = { count: listed.length, ids: listed.map((session) => session.id) };
		}
		out[name] = record;
	} finally {
		rmSync(dir, { recursive: true, force: true });
	}
}
console.log(JSON.stringify(out, null, 2));
