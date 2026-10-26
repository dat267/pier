// Reference: pi v1.4.0 (4a080252), core/compaction/utils.ts
// extractFileOpsFromMessage/computeFileLists/formatFileOperations.
// Regenerate from the repository root:
// node --experimental-strip-types coding/testdata/file-ops/generate.mjs ./pi > coding/testdata/file-ops/golden.json
import { registerHooks } from "node:module";
import { resolve } from "node:path";
import { pathToFileURL } from "node:url";
const root = resolve(process.argv[2] ?? "./pi");
registerHooks({ resolve(specifier, context, next) {
	const map = {
		"@earendil-works/pi-telemetry": "packages/telemetry/src/index.ts",
		"@earendil-works/pi-ai": "packages/ai/src/index.ts",
		"@earendil-works/pi-agent-core": "packages/agent/src/index.ts",
	};
	return map[specifier] ? { url: pathToFileURL(resolve(root, map[specifier])).href, shortCircuit: true } : next(specifier, context);
} });
const { createFileOps, extractFileOpsFromMessage, computeFileLists, formatFileOperations } = await import(pathToFileURL(resolve(root, "packages/coding-agent/src/core/compaction/utils.ts")));

const assistant = (calls) => ({ role: "assistant", content: calls.map(([name, args], i) => ({ type: "toolCall", id: `c${i}`, name, arguments: args })), api: "anthropic-messages", provider: "anthropic", model: "m", usage: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0, totalTokens: 0, cost: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0, total: 0 } }, stopReason: "toolUse", timestamp: 1 });
const result = (calls) => ({ role: "toolResult", toolCallId: "call", toolName: "bash", content: [{ type: "text", text: "ok" }], isError: false, timestamp: 2, ...(calls ? { nestedCalls: { calls: calls.map(([name, args], i) => ({ name, arguments: args, toolCallId: `n${i}` })), complete: true } } : {}) });

const cases = {
	"read-only": [assistant([["read", { path: "a.ts" }], ["read", { path: "b.ts" }]])],
	"modified": [assistant([["write", { path: "w.ts" }], ["edit", { path: "e.ts" }]])],
	"read-then-edited": [assistant([["read", { path: "x.ts" }], ["edit", { path: "x.ts" }]])],
	"read-then-written": [assistant([["read", { path: "y.ts" }], ["write", { path: "y.ts" }]])],
	"duplicates": [assistant([["read", { path: "d.ts" }], ["read", { path: "d.ts" }], ["edit", { path: "m.ts" }], ["edit", { path: "m.ts" }]])],
	"missing-path": [assistant([["read", {}], ["edit", { path: 5 }], ["write", { path: null }]])],
	"unknown-tool": [assistant([["grep", { path: "z.ts" }], ["read", { path: "keep.ts" }]])],
	"nested-calls": [result([["read", { path: "n1.ts" }], ["write", { path: "n2.ts" }]])],
	"mixed": [assistant([["read", { path: "a" }], ["write", { path: "b" }], ["edit", { path: "c" }]]), result([["edit", { path: "c" }]]), assistant([["read", { path: "d" }]])],
	"astral-order": [assistant([["read", { path: "😀.ts" }], ["read", { path: "ｚ.ts" }], ["read", { path: "Z.ts" }]])],
	"empty": [],
};
const fixtures = Object.entries(cases).map(([name, messages]) => {
	const ops = createFileOps();
	for (const message of messages) extractFileOpsFromMessage(message, ops);
	const { readFiles, modifiedFiles } = computeFileLists(ops);
	return { name, messages, readFiles, modifiedFiles, formatted: formatFileOperations(readFiles, modifiedFiles) };
});
console.log(JSON.stringify(fixtures, null, 2));
