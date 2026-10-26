// Reference: pi v1.4.0 (4a080252), core/compaction/utils.ts serializeConversation
// (called after convertToLlm, so it receives LLM messages).
// Regenerate from the repository root:
// node --experimental-strip-types coding/testdata/serialize-conversation/generate.mjs ./pi > coding/testdata/serialize-conversation/golden.json
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
const { serializeConversation } = await import(pathToFileURL(resolve(root, "packages/coding-agent/src/core/compaction/utils.ts")));

const user = (content) => ({ role: "user", content, timestamp: 1 });
const assistant = (content) => ({ role: "assistant", content, api: "anthropic-messages", provider: "anthropic", model: "m", usage: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0, totalTokens: 0, cost: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0, total: 0 } }, stopReason: "stop", timestamp: 2 });
const toolResult = (content) => ({ role: "toolResult", toolCallId: "call-1", toolName: "read", content, isError: false, timestamp: 3 });
const toolCall = (id, name, args) => ({ type: "toolCall", id, name, arguments: args });
const long = (unit, count) => unit.repeat(count);

const cases = {
	"user-string": [user("hello")],
	"user-blocks": [user([{ type: "text", text: "one" }, { type: "image", data: "aW1n", mimeType: "image/png" }, { type: "text", text: "two" }])],
	"user-empty-blocks": [user([])],
	"user-crlf": [user("a\r\nb")],
	"assistant-text": [assistant([{ type: "text", text: "hi" }])],
	"assistant-thinking-and-text": [assistant([{ type: "thinking", thinking: "plan" }, { type: "text", text: "answer" }])],
	"assistant-args-order": [assistant([toolCall("c1", "lookup", { z: 1, a: { b: [1, 2], c: null }, "<&>": "x<y>&z" })])],
	"assistant-call-only": [assistant([toolCall("c1", "read", { path: "a.txt" })])],
	"assistant-multiple-calls": [assistant([toolCall("c1", "read", { path: "a" }), toolCall("c2", "grep", { pattern: "x", limit: 2 })])],
	"assistant-empty": [assistant([])],
	"tool-short": [toolResult([{ type: "text", text: "done" }])],
	"tool-empty": [toolResult([])],
	"tool-image-only": [toolResult([{ type: "image", data: "aW1n", mimeType: "image/png" }])],
	"tool-long-ascii": [toolResult([{ type: "text", text: long("x", 2500) }])],
	"tool-long-astral": [toolResult([{ type: "text", text: long("😀", 1500) }])],
	"mixed": [user("start"), assistant([{ type: "text", text: "working" }, toolCall("c1", "read", { path: "a" })]), toolResult([{ type: "text", text: "contents" }]), assistant([{ type: "text", text: "done" }])],
};
const fixtures = Object.entries(cases).map(([name, messages]) => ({ name, messages, expected: serializeConversation(messages) }));
console.log(JSON.stringify(fixtures, null, 2));
