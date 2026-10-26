// Reference: pi v1.4.0 (4a080252), core/messages.ts convertToLlm.
// D248 deliberately changes one ordering (custom messages landing between a tool
// call and its result), so those inputs are not part of this corpus.
// Regenerate from the repository root:
// node --experimental-strip-types coding/testdata/convert-to-llm/generate.mjs ./pi > coding/testdata/convert-to-llm/golden.json
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
const { convertToLlm } = await import(pathToFileURL(resolve(root, "packages/coding-agent/src/core/messages.ts")));

const usage = { input: 0, output: 0, cacheRead: 0, cacheWrite: 0, totalTokens: 0, cost: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0, total: 0 } };
const system = (content, extra = {}) => ({ role: "system", content, timestamp: 0, ...extra });
const user = (content) => ({ role: "user", content, timestamp: 1 });
const assistant = (content) => ({ role: "assistant", content, api: "anthropic-messages", provider: "anthropic", model: "m", usage, stopReason: "stop", timestamp: 2 });
const toolResult = (content) => ({ role: "toolResult", toolCallId: "c1", toolName: "read", content, isError: false, timestamp: 3 });
const bash = (command, output, extra = {}) => ({ role: "bashExecution", command, output, exitCode: 0, cancelled: false, truncated: false, timestamp: 4, ...extra });
const custom = (content, extra = {}) => ({ role: "custom", customType: "note", content, display: false, timestamp: 5, ...extra });
const branchSummary = (summary) => ({ role: "branchSummary", summary, fromId: "e1", timestamp: 6 });
const compactionSummary = (summary) => ({ role: "compactionSummary", summary, tokensBefore: 100, timestamp: 7 });

const cases = {
	"passthrough": [system("prompt"), user("hi"), assistant([{ type: "text", text: "hello" }]), toolResult([{ type: "text", text: "out" }])],
	"bash-included": [bash("ls", "file.txt")],
	"bash-excluded": [bash("ls", "file.txt", { excludeFromContext: true }), user("after")],
	"bash-cancelled": [bash("sleep 9", "partial", { cancelled: true })],
	"bash-truncated": [bash("yes", "line\n", { truncated: true })],
	"custom-string": [custom("a note")],
	"custom-blocks": [custom([{ type: "text", text: "text" }, { type: "image", data: "aW1n", mimeType: "image/png" }])],
	"branch-summary": [branchSummary("summary text")],
	"compaction-summary": [compactionSummary("compact text")],
	"mixed-order": [user("start"), bash("cmd", "out"), custom("note"), branchSummary("branch"), compactionSummary("compact"), assistant([{ type: "text", text: "done" }])],
	"empty": [],
};
const canonical = (value) => {
	if (Array.isArray(value)) return value.map(canonical);
	if (value && typeof value === "object") {
		const out = {};
		for (const key of Object.keys(value).sort()) out[key] = canonical(value[key]);
		return out;
	}
	return value;
};
const fixtures = Object.entries(cases).map(([name, messages]) => ({
	name, messages, expected: canonical(convertToLlm(messages)),
}));
console.log(JSON.stringify(fixtures, null, 2));
