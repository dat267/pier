// Regenerate from the repository root:
// node coding/testdata/branch-preparation/generate.mjs ./pi > coding/testdata/branch-preparation/golden.json
// Reference: pi v1.4.0 (4a080252), packages/coding-agent/src/core/compaction/
// branch-summarization.ts prepareBranchEntries and core/messages.ts convertToLlm.
// Executes the pinned functions, never an installed bundle.
import { resolve, join } from "node:path";
import { pathToFileURL } from "node:url";

const root = resolve(process.argv[2] ?? "./pi");
const { prepareBranchEntries } = await import(
	pathToFileURL(join(root, "packages/coding-agent/src/core/compaction/branch-summarization.ts")).href,
);
const { convertToLlm } = await import(
	pathToFileURL(join(root, "packages/coding-agent/src/core/messages.ts")).href,
);

const ts = "2026-01-01T00:00:00.000Z";
const entry = (id, type, extra) => ({ id, parentId: null, timestamp: ts, type, ...extra });
const user = (id, text) => entry(id, "message", { message: { role: "user", timestamp: 1767225600000, content: [{ type: "text", text }] } });
const assistant = (id, text, calls = []) => entry(id, "message", { message: {
	role: "assistant", timestamp: 1767225600000, stopReason: "stop", api: "anthropic-messages",
	content: [...(text ? [{ type: "text", text }] : []), ...calls],
} });
const toolCall = (id, name, args) => ({ type: "toolCall", id, name, arguments: args });
const toolResult = (id, callId) => entry(id, "message", { message: { role: "toolResult", toolCallId: callId, toolName: "read", timestamp: 1767225600000, content: [{ type: "text", text: "out" }] } });
const bash = (id, command, output, extra = {}) => entry(id, "message", { message: {
	role: "bashExecution", command, output, exitCode: 0, cancelled: false, truncated: false,
	timestamp: 1767225600000, ...extra,
} });
const customMessage = (id, customType, content) => entry(id, "custom_message", { customType, content, display: true });
const branchSummary = (id, summary, fromHook, details) => entry(id, "branch_summary", { summary, fromId: "x", fromHook, details });
const compaction = (id, summary, tokensBefore) => entry(id, "compaction", { summary, tokensBefore });
const other = (id, type, extra = {}) => entry(id, type, extra);

const cases = {
	"bash-execution": [
		user("u1", "hello"),
		bash("b1", "!ls -la", "file1\nfile2"),
		assistant("a1", "done"),
	],
	"bash-excluded": [
		user("u1", "hello"),
		bash("b1", "!!secret", "hidden", { excludeFromContext: true }),
		assistant("a1", "done"),
	],
	"bash-execution-details": [
		bash("b1", "!npm test", "line1\nline2\nline3", { exitCode: 2, truncated: true, fullOutputPath: "/tmp/full.txt" }),
	],
	"bash-only": [bash("b1", "!pwd", "/work")],
	"custom-message": [user("u1", "hello"), customMessage("c1", "note", "remember this"), assistant("a1", "done")],
	"summaries": [
		user("u1", "start"),
		branchSummary("s1", "branch summary text", false, { readFiles: ["/r"], modifiedFiles: ["/m"] }),
		compaction("k1", "compaction summary text", 1234),
		user("u2", "after"),
	],
	"tool-files": [
		user("u1", "go"),
		assistant("a1", "", [toolCall("t1", "read", { path: "/r" }), toolCall("t2", "write", { path: "/w" }), toolCall("t3", "edit", { path: "/e" })]),
		toolResult("r1", "t1"),
	],
	"budget-tiny": [user("u1", "a".repeat(400)), assistant("a1", "b".repeat(400)), user("u2", "recent")],
	"budget-summary-fits": [user("u1", "c".repeat(4000)), compaction("k1", "short summary", 100), user("u2", "d".repeat(400))],
	"ignored-entries": [
		user("u1", "hello"),
		other("o1", "thinking_level_change", { thinkingLevel: "high" }),
		other("o2", "model_change", { provider: "p", modelId: "m" }),
		other("o3", "custom", { customType: "x", data: {} }),
		other("o4", "label", { targetId: "u1", label: "L" }),
		other("o5", "session_info", { name: "n" }),
		assistant("a1", "hi"),
	],
};

const textOf = (content) => {
	if (typeof content === "string") return content;
	return content.filter((block) => block.type === "text").map((block) => block.text).join("\n");
};

const out = {};
for (const [name, entries] of Object.entries(cases)) {
	const budget = name === "budget-tiny" ? 120 : name === "budget-summary-fits" ? 200 : 0;
	const preparation = prepareBranchEntries(entries, budget);
	const converted = convertToLlm(preparation.messages).map((message) => ({
		role: message.role,
		text: message.content === undefined ? "" : textOf(message.content),
	}));
	out[name] = {
		entries,
		budget,
		preparedRoles: preparation.messages.map((message) => message.role),
		converted,
		fileOps: {
			read: [...preparation.fileOps.read].sort(),
			written: [...preparation.fileOps.written].sort(),
			edited: [...preparation.fileOps.edited].sort(),
		},
		totalTokens: preparation.totalTokens,
	};
}
console.log(JSON.stringify(out, null, 2));
