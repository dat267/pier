// Reference: pi v1.4.0 (4a080252), core/compaction/compaction.ts findCutPoint.
// Regenerate from the repository root:
// node --experimental-strip-types coding/testdata/cut-point/generate.mjs ./pi > coding/testdata/cut-point/golden.json
import { registerHooks } from "node:module";
import { resolve } from "node:path";
import { pathToFileURL } from "node:url";
const root = resolve(process.argv[2] ?? "./pi");
registerHooks({ resolve(specifier, context, next) {
	const map = {
		"@earendil-works/pi-telemetry": "packages/telemetry/src/index.ts",
		"@earendil-works/pi-ai": "packages/ai/src/index.ts",
		"@earendil-works/pi-ai/compat": "packages/ai/src/compat.ts",
		"@earendil-works/pi-agent-core": "packages/agent/src/index.ts",
	};
	return map[specifier] ? { url: pathToFileURL(resolve(root, map[specifier])).href, shortCircuit: true } : next(specifier, context);
} });
const { findCutPoint } = await import(pathToFileURL(resolve(root, "packages/coding-agent/src/core/compaction/compaction.ts")));

let counter = 0;
const base = (type, extra = {}) => ({ type, id: `e${++counter}`, parentId: counter > 1 ? `e${counter - 1}` : null, timestamp: "2026-01-01T00:00:00.000Z", ...extra });
const user = (text) => base("message", { message: { role: "user", content: text, timestamp: 1 } });
const assistantText = (text) => base("message", { message: { role: "assistant", content: [{ type: "text", text }], api: "anthropic-messages", provider: "anthropic", model: "m", usage: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0, totalTokens: 0, cost: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0, total: 0 } }, stopReason: "stop", timestamp: 2 } });
const assistantCall = (name, args) => base("message", { message: { role: "assistant", content: [{ type: "toolCall", id: "c1", name, arguments: args }], api: "anthropic-messages", provider: "anthropic", model: "m", usage: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0, totalTokens: 0, cost: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0, total: 0 } }, stopReason: "toolUse", timestamp: 2 } });
const toolResult = (text) => base("message", { message: { role: "toolResult", toolCallId: "c1", toolName: "read", content: [{ type: "text", text }], isError: false, timestamp: 3 } });
const system = () => base("message", { message: { role: "system", content: "prompt", timestamp: 0 } });
const label = () => base("label", { targetId: "e1", label: "x" });
const thinkingChange = () => base("thinking_level_change", { thinkingLevel: "high" });
const compaction = () => base("compaction", { summary: "old", firstKeptEntryId: "e1", tokensBefore: 100, timestamp: "2026-01-01T00:00:00.000Z" });
const branchSummary = () => base("branch_summary", { summary: "branch", fromId: "e1", timestamp: "2026-01-01T00:00:00.000Z" });
const customMessage = () => base("custom_message", { customType: "note", content: "hello custom", display: false, timestamp: "2026-01-01T00:00:00.000Z" });
const contextEdit = () => base("context_edit", { targetId: "e2", replacement: null, timestamp: "2026-01-01T00:00:00.000Z" });

const big = "x".repeat(4000); // 1000 estimated tokens per message
const medium = "y".repeat(400);
const scenarios = [];
const push = (name, entries, params) => scenarios.push({ name, entries, params });

push("single-short", [user("hi")], [{ startIndex: 0, endIndex: 1, keepRecentTokens: 100 }]);
push("user-assistant-pair", [user(big), assistantText(big)], [{ startIndex: 0, endIndex: 2, keepRecentTokens: 100 }, { startIndex: 0, endIndex: 2, keepRecentTokens: 2000 }]);
push("three-pairs-budget", [user(big), assistantText(big), user(big), assistantText(big), user(big), assistantText(big)], [
	{ startIndex: 0, endIndex: 6, keepRecentTokens: 100 },
	{ startIndex: 0, endIndex: 6, keepRecentTokens: 1500 },
	{ startIndex: 0, endIndex: 6, keepRecentTokens: 2500 },
	{ startIndex: 0, endIndex: 6, keepRecentTokens: 10000 },
]);
push("tool-pair", [user(medium), assistantCall("read", { path: "a" }), toolResult(big), user(big), assistantText(big)], [
	{ startIndex: 0, endIndex: 5, keepRecentTokens: 500 },
	{ startIndex: 0, endIndex: 5, keepRecentTokens: 1200 },
]);
push("metadata-entries", [user(big), assistantText(big), label(), thinkingChange(), user(big), assistantText(big)], [
	{ startIndex: 0, endIndex: 6, keepRecentTokens: 1200 },
	{ startIndex: 0, endIndex: 6, keepRecentTokens: 2500 },
]);
push("system-between", [user(big), system(), assistantText(big), user(big), assistantText(big)], [{ startIndex: 0, endIndex: 5, keepRecentTokens: 1200 }]);
push("compaction-entry", [user(big), assistantText(big), compaction(), user(big), assistantText(big)], [{ startIndex: 0, endIndex: 5, keepRecentTokens: 1200 }]);
push("branch-and-custom", [user(big), branchSummary(), customMessage(), user(big)], [{ startIndex: 0, endIndex: 4, keepRecentTokens: 300 }]);
push("context-edit", [user(big), contextEdit(), assistantText(big)], [{ startIndex: 0, endIndex: 3, keepRecentTokens: 100 }]);
push("window-subset", [user(big), assistantText(big), user(big), assistantText(big), user(big), assistantText(big)], [
	{ startIndex: 2, endIndex: 6, keepRecentTokens: 100 },
	{ startIndex: 2, endIndex: 5, keepRecentTokens: 1500 },
]);
push("empty-window", [user(big), assistantText(big)], [{ startIndex: 1, endIndex: 1, keepRecentTokens: 100 }]);
push("split-turn", [user(big), assistantCall("read", { path: "a" }), toolResult(medium), assistantCall("read", { path: "b" }), toolResult(big)], [{ startIndex: 0, endIndex: 5, keepRecentTokens: 800 }]);

const fixtures = scenarios.map((scenario) => ({
	name: scenario.name,
	entries: scenario.entries,
	cases: scenario.params.map((params) => ({ ...params, expected: findCutPoint(scenario.entries, params.startIndex, params.endIndex, params.keepRecentTokens) })),
}));
console.log(JSON.stringify(fixtures, null, 2));
