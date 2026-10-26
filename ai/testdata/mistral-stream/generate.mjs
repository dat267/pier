// Reference: pi v1.4.0 (4a080252), packages/ai/src/api/mistral-conversations.ts.
// Regenerate: node --experimental-strip-types ai/testdata/mistral-stream/generate.mjs ./pi > ai/testdata/mistral-stream/golden.json
import { registerHooks } from "node:module";
import { resolve } from "node:path";
import { pathToFileURL } from "node:url";
const root = resolve(process.argv[2] ?? "./pi");
registerHooks({ resolve(specifier, context, next) {
	return specifier === "@earendil-works/pi-telemetry"
		? { url: pathToFileURL(resolve(root, "packages/telemetry/src/index.ts")).href, shortCircuit: true }
		: next(specifier, context);
} });
const { stream } = await import(pathToFileURL(resolve(root, "packages/ai/src/api/mistral-conversations.ts")));
const chunk = (delta, finish_reason, extra = {}) => ({ choices: [{ delta, ...(finish_reason ? { finish_reason } : {}) }], ...extra });
const call = (index, args, extra = {}) => ({ index, function: { name: "lookup", arguments: args }, ...extra });
const thought = (text) => ({ type: "thinking", thinking: [{ type: "text", text }] });
const scenarios = [
	{ name: "text", chunks: [chunk({ content: "Hel" }, undefined, { id: "first" }), chunk({ content: "lo" }, "stop", { id: "second" })] },
	{ name: "thinking-text", chunks: [chunk({ content: [thought("plan"), { type: "text", text: "answer" }] }, "stop")] },
	{ name: "empty-deltas", chunks: [chunk({ content: [thought("plan")] }), chunk({ content: "" }), chunk({ content: [thought(" more")] }), chunk({ content: [{ type: "text", text: "" }] }, "stop")] },
	{ name: "function-call", chunks: [chunk({ tool_calls: [call(0, '{"pa', { id: "abc123XYZ" })] }), chunk({ tool_calls: [call(0, 'th":"a"}')] }, "tool_calls")] },
	{ name: "parallel-calls", chunks: [chunk({ tool_calls: [call(0, '{"x":1}', { id: "call0" }), call(1, '{"x":2}', { id: "call1" }), call(2, '{"x":3}', { id: "call2" })] }, "tool_calls")] },
	{ name: "continued-call-after-text", chunks: [chunk({ tool_calls: [call(0, '{"pa', { id: "abc123XYZ" })] }), chunk({ content: "middle" }), chunk({ tool_calls: [call(0, 'th":"a"}')] }), chunk({ content: "after" }, "tool_calls")] },
	{ name: "continued-call-after-thinking", chunks: [chunk({ tool_calls: [call(0, '{"pa', { id: "abc123XYZ" })] }), chunk({ content: [thought("middle")] }), chunk({ tool_calls: [call(0, 'th":"a"}')] }, "tool_calls")] },
	{ name: "object-arguments", chunks: [chunk({ tool_calls: [call(0, { z: 1, a: 2 }, { id: "call0" })] }, "tool_calls")] },
	{ name: "null-arguments", chunks: [chunk({ tool_calls: [call(0, null, { id: "call0" })] }, "tool_calls")] },
	{ name: "absent-arguments", chunks: [chunk({ tool_calls: [call(0, undefined, { id: "call0" })] }, "tool_calls")] },
	{ name: "usage", chunks: [chunk({ content: "hi" }, "stop", { usage: { prompt_tokens: 30, completion_tokens: 7, total_tokens: 37, prompt_tokens_details: { cached_tokens: 10 } } })] },
	{ name: "usage-only", chunks: [chunk({ content: "hi" }, "stop"), { choices: [], usage: { prompt_tokens: 5, completion_tokens: 2 } }] },
	{ name: "negative-cache", chunks: [chunk({}, "stop", { usage: { prompt_tokens: 5, completion_tokens: 2, num_cached_tokens: -1 } })] },
	{ name: "oversized-cache", chunks: [chunk({}, "stop", { usage: { prompt_tokens: 5, completion_tokens: 2, num_cached_tokens: 20 } })] },
	{ name: "length", chunks: [chunk({ content: "cut" }, "length")] },
	{ name: "model-length", chunks: [chunk({ content: "cut" }, "model_length")] },
	{ name: "error", chunks: [chunk({ content: "partial" }, "error")] },
	{ name: "unknown-finish", chunks: [chunk({ content: "partial" }, "safety")] },
	{ name: "missing-finish", chunks: [chunk({ content: "partial" })] },
	{ name: "empty-stream", chunks: [] },
	{ name: "crlf", separator: "\r\n", chunks: [chunk({ content: "ok" }, "stop")] },
	{ name: "unterminated", noDone: true, unterminated: true, chunks: [chunk({ content: "ok" }, "stop")] },
];
const indexed = new Set(["text_start", "text_delta", "text_end", "thinking_start", "thinking_delta", "thinking_end", "toolcall_start", "toolcall_delta", "toolcall_end"]);
function canonical(value) {
	if (Array.isArray(value)) return value.map(canonical);
	if (value && typeof value === "object") return Object.fromEntries(Object.keys(value).sort().map(key => [key, canonical(value[key])]));
	return value;
}
function projectContent(block) {
	const out = { type: block.type };
	if (block.type === "text") out.text = block.text;
	if (block.type === "thinking") { out.thinking = block.thinking; out.thinkingSignature = block.thinkingSignature ?? ""; }
	if (block.type === "toolCall") Object.assign(out, { id: block.id, name: block.name, arguments: canonical(block.arguments ?? {}) });
	return out;
}
function projectMessage(message) {
	const out = { content: message.content.map(projectContent), api: message.api, provider: message.provider, model: message.model, stopReason: message.stopReason, usage: canonical(message.usage) };
	for (const key of ["responseId", "rawStopReason", "errorMessage"]) if (message[key] !== undefined) out[key] = message[key];
	return out;
}
function project(event) {
	const out = { type: event.type };
	if (indexed.has(event.type)) out.contentIndex = event.contentIndex;
	for (const key of ["delta", "content", "reason"]) if (event[key] !== undefined) out[key] = event[key];
	if (event.toolCall !== undefined) out.toolCall = { id: event.toolCall.id, name: event.toolCall.name, arguments: canonical(event.toolCall.arguments ?? {}) };
	if (event.message !== undefined) out.message = projectMessage(event.message);
	if (event.error !== undefined) out.error = projectMessage(event.error);
	return out;
}
const model = { api: "mistral-conversations", provider: "mistral", id: "mistral-large-latest", name: "Mistral Large", baseUrl: "https://invalid.test", reasoning: true, input: ["text"], cost: { input: 2, output: 6, cacheRead: 0.5, cacheWrite: 0 } };
const fixtures = [];
for (const scenario of scenarios) {
	const newline = scenario.separator ?? "\n";
	let body = scenario.chunks.map(value => `data: ${JSON.stringify(value)}${newline}${newline}`).join("");
	if (scenario.unterminated) body = body.trimEnd();
	if (!scenario.noDone) body += `data: [DONE]${newline}${newline}`;
	globalThis.fetch = async () => new Response(body, { headers: { "content-type": "text/event-stream" } });
	const events = [];
	for await (const event of stream(model, { messages: [{ role: "user", content: "hi", timestamp: 1 }] }, { apiKey: "test-key", env: {} })) events.push(project(event));
	fixtures.push({ name: scenario.name, sse: body, events });
}
console.log(JSON.stringify(fixtures, null, 2));
