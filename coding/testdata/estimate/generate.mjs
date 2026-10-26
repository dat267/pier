// Reference: pi v1.4.0 (4a080252), core/compaction/compaction.ts
// estimateContextTokens (and its estimateTokens helper).
// Regenerate from the repository root:
// node --experimental-strip-types coding/testdata/estimate/generate.mjs ./pi > coding/testdata/estimate/golden.json
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
const { estimateContextTokens } = await import(pathToFileURL(resolve(root, "packages/coding-agent/src/core/compaction/compaction.ts")));

const usage = (fields) => ({ input: 0, output: 0, cacheRead: 0, cacheWrite: 0, totalTokens: 0, cost: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0, total: 0 }, ...fields });
const assistant = (content, extra = {}) => ({ role: "assistant", content, api: "anthropic-messages", provider: "anthropic", model: "m", usage: usage({}), stopReason: "stop", timestamp: 2, ...extra });
const user = (content) => ({ role: "user", content, timestamp: 1 });
const toolResult = (content) => ({ role: "toolResult", toolCallId: "c1", toolName: "read", content, isError: false, timestamp: 3 });
const system = (extra) => ({ role: "system", content: "base prompt", timestamp: 0, ...extra });

const cases = {
	"user-string": [user("hello")],
	"user-astral": [user("😀😀😀")],
	"user-image": [user([{ type: "text", text: "look" }, { type: "image", data: "aW1n", mimeType: "image/png" }])],
	"system-sections-tools": [system({ sections: { alpha: "xxxx", beta: "yyyyy" }, toolsAdded: [{ name: "read", description: "d", parameters: { type: "object" } }] })],
	"assistant-blocks": [assistant([{ type: "text", text: "answer" }, { type: "thinking", thinking: "plan" }, { type: "toolCall", id: "c1", name: "read", arguments: { path: "a" } }])],
	"tool-result": [toolResult([{ type: "text", text: "contents" }, { type: "image", data: "aW1n", mimeType: "image/png" }])],
	"custom": [{ role: "custom", customType: "note", content: "custom text", display: false, timestamp: 4 }],
	"usage-total": [user("hi"), assistant([{ type: "text", text: "ok" }], { usage: usage({ input: 100, output: 10, totalTokens: 110 }) })],
	"usage-components": [user("hi"), assistant([{ type: "text", text: "ok" }], { usage: usage({ input: 100, output: 10, cacheRead: 5, cacheWrite: 3 }) })],
	"usage-ignored-aborted": [user("hi"), assistant([{ type: "text", text: "ok" }], { usage: usage({ totalTokens: 500 }), stopReason: "aborted" })],
	"usage-ignored-error": [user("hi"), assistant([{ type: "text", text: "ok" }], { usage: usage({ totalTokens: 500 }), stopReason: "error" })],
	"usage-zero": [user("hi"), assistant([{ type: "text", text: "ok" }], { usage: usage({}) })],
	"trailing-after-usage": [
		user("question"),
		assistant([{ type: "text", text: "answer" }], { usage: usage({ input: 50, output: 5, totalTokens: 55 }) }),
		toolResult([{ type: "text", text: "tool output" }]),
		user("follow up"),
	],
	"empty": [],
};
const fixtures = Object.entries(cases).map(([name, messages]) => ({
	name,
	messages,
	expected: estimateContextTokens(messages),
}));
console.log(JSON.stringify(fixtures, null, 2).replace(/"lastUsageIndex": null/g, '"lastUsageIndex": null'));
