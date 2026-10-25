// Regenerate from the repository root:
// node coding/testdata/session-list/generate.mjs ./pi > coding/testdata/session-list/golden.json
// Reference: pi v1.4.0 (4a080252), packages/coding-agent/src/core/session-manager.ts
// (buildSessionInfo, getMessageActivityTime, SessionManager.list). Executes the
// pinned lister over temp session files, never an installed bundle.
import { resolve, join } from "node:path";
import { pathToFileURL } from "node:url";
import { mkdtempSync, writeFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";

const root = resolve(process.argv[2] ?? "./pi");
const { SessionManager } = await import(
	pathToFileURL(join(root, "packages/coding-agent/src/core/session-manager.ts")).href,
);

const base = 1767225600000;
const ts = (ms) => new Date(base + ms).toISOString();
const header = (id) => JSON.stringify({ type: "session", id, timestamp: ts(0), cwd: "/work" });
const user = (id, ms, text) => JSON.stringify({ type: "message", id, parentId: null, timestamp: ts(ms),
	message: { role: "user", timestamp: base + ms, content: [{ type: "text", text }] } });
const assistant = (id, ms, text) => JSON.stringify({ type: "message", id, parentId: null, timestamp: ts(ms),
	message: { role: "assistant", timestamp: base + ms, content: [{ type: "text", text }], stopReason: "stop", api: "anthropic-messages" } });
const assistantNoContent = (id, ms) => JSON.stringify({ type: "message", id, parentId: null, timestamp: ts(ms),
	message: { role: "assistant", timestamp: base + ms } });
const toolResult = (id, ms) => JSON.stringify({ type: "message", id, parentId: null, timestamp: ts(ms),
	message: { role: "toolResult", toolCallId: "c1", toolName: "read", timestamp: base + ms, content: [{ type: "text", text: "result" }] } });
const custom = (id, ms) => JSON.stringify({ type: "message", id, parentId: null, timestamp: ts(ms),
	message: { role: "custom", customType: "note", timestamp: base + ms, content: [{ type: "text", text: "custom" }] } });
const systemMessage = (id, ms) => JSON.stringify({ type: "message", id, parentId: null, timestamp: ts(ms),
	message: { role: "system", timestamp: base + ms, content: [{ type: "text", text: "system" }] } });
const userNoContent = (id, ms) => JSON.stringify({ type: "message", id, parentId: null, timestamp: ts(ms),
	message: { role: "user", timestamp: base + ms } });
const assistantNoTimestamp = (id, ms, text) => JSON.stringify({ type: "message", id, parentId: null, timestamp: ts(ms),
	message: { role: "assistant", content: [{ type: "text", text }] } });
const sessionInfo = (ms, name) => JSON.stringify({ type: "session_info", id: "i1", parentId: null, timestamp: ts(ms), name });

const cases = {
	"assistant-last": [header("s1"), user("m1", 1000, "hello"), assistant("m2", 2000, "hi there")],
	"tool-result-last": [header("s1"), user("m1", 1000, "hello"), assistant("m2", 2000, "calling"), toolResult("m3", 3000)],
	"custom-last": [header("s1"), user("m1", 1000, "hello"), assistant("m2", 2000, "hi"), custom("m3", 3000)],
	"system-last": [header("s1"), user("m1", 1000, "hello"), assistant("m2", 2000, "hi"), systemMessage("m3", 3000)],
	"assistant-no-content": [header("s1"), user("m1", 1000, "hello"), assistantNoContent("m2", 2000), toolResult("m3", 3000)],
	"user-no-content-last": [header("s1"), assistant("m1", 2000, "hi"), userNoContent("m2", 3000)],
	"assistant-no-message-timestamp": [header("s1"), user("m1", 1000, "hello"), assistantNoTimestamp("m2", 2000, "no timestamp")],
	"no-activity": [header("s1"), toolResult("m1", 3000)],
	"empty-name": [header("s1"), user("m1", 1000, "hello"), sessionInfo(1500, "  ")],
};

const out = {};
for (const [name, lines] of Object.entries(cases)) {
	const dir = mkdtempSync(join(tmpdir(), "pier-sessions-"));
	try {
		writeFileSync(join(dir, "session.jsonl"), lines.join("\n") + "\n");
		const sessions = await SessionManager.list("/work", dir);
		out[name] = {
			lines,
			sessions: sessions.map((session) => ({
				id: session.id,
				cwd: session.cwd,
				name: session.name ?? null,
				parentSessionPath: session.parentSessionPath ?? null,
				created: session.created.toISOString(),
				modified: session.modified.toISOString(),
				messageCount: session.messageCount,
				firstMessage: session.firstMessage,
				allMessagesText: session.allMessagesText,
			})),
		};
	} finally {
		rmSync(dir, { recursive: true, force: true });
	}
}
console.log(JSON.stringify(out, null, 2));
