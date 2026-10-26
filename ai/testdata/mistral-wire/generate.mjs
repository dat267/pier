// Regenerate: node --experimental-strip-types ai/testdata/mistral-wire/generate.mjs ./pi > ai/testdata/mistral-wire/golden.json
import { registerHooks } from "node:module";
import { resolve } from "node:path";
import { pathToFileURL } from "node:url";
const root = resolve(process.argv[2]);
registerHooks({ resolve(s, c, next) {
	return s === "@earendil-works/pi-telemetry"
		? { url: pathToFileURL(resolve(root, "packages/telemetry/src/index.ts")).href, shortCircuit: true }
		: next(s, c);
} });
const { stream } = await import(pathToFileURL(resolve(root, "packages/ai/src/api/mistral-conversations.ts")));
const cases = [{ name: "minimal", messages: [{ role: "user", content: "hello", timestamp: 1 }], options: {} }, {
	name: "image-and-tools",
	messages: [
		{ role: "system", content: "system", toolsAdded: [{ name: "lookup", description: "Look up", parameters: { type: "object", properties: { z: { type: "string" }, a: { type: "number" } }, required: ["z", "a"] }, constrainedSampling: { type: "json_schema", strict: "prefer" } }], timestamp: 1 },
		{ role: "user", content: [{ type: "text", text: "look" }, { type: "image", data: "aW1hZ2U=", mimeType: "image/png" }], timestamp: 2 },
		{ role: "assistant", content: [{ type: "text", text: "search" }, { type: "thinking", thinking: "think" }, { type: "toolCall", id: "call-1", name: "lookup", arguments: { z: 1, a: 2 } }], stopReason: "toolUse", timestamp: 3 },
		{ role: "toolResult", toolCallId: "call-1", toolName: "lookup", content: [{ type: "text", text: "found" }], isError: false, timestamp: 4 },
	],
	options: { temperature: 0.5, maxTokens: 16, toolChoice: "auto", promptMode: "reasoning", reasoningEffort: "high", sessionId: "session" },
}, {
	name: "payload-replacement",
	messages: [{ role: "user", content: "hello", timestamp: 1 }], options: {},
	replacement: {
		model: "mistral-small-latest", stream: true,
		messages: [{ role: "user", content: [{ type: "document_url", documentUrl: "https://example.test/document", documentName: "report" }] }],
		topP: 0, maxTokens: 0, randomSeed: 0, responseFormat: { type: "json_schema", jsonSchema: { name: "Output", schemaDefinition: { type: "object", properties: { z: { type: "string" }, a: { type: "number" } } }, strict: true } },
		toolChoice: "none", presencePenalty: 0, frequencyPenalty: 0, parallelToolCalls: false,
		reasoningEffort: "max", promptMode: "reasoning", promptCacheKey: "cache-key",
		safePrompt: false, extra: { z: 1, a: 2 },
	},
}, {
	name: "named-tool-choice", messages: [{ role: "user", content: "hello", timestamp: 1 }], options: {}, forcedTool: "lookup",
}, {
	name: "thinking-only-assistant",
	messages: [
		{ role: "user", content: "hi", timestamp: 1 },
		{ role: "assistant", content: [{ type: "thinking", thinking: "plan" }], stopReason: "stop", timestamp: 2 },
	], options: {},
}, {
	name: "zero-argument-tool-call",
	messages: [
		{ role: "system", content: "sys", toolsAdded: [{ name: "now", description: "Time", parameters: { type: "object", properties: {} } }], timestamp: 1 },
		{ role: "user", content: "time?", timestamp: 2 },
		{ role: "assistant", content: [{ type: "toolCall", id: "c1", name: "now", arguments: {} }], stopReason: "toolUse", timestamp: 3 },
		{ role: "toolResult", toolCallId: "c1", toolName: "now", content: [{ type: "text", text: "12:00" }], isError: false, timestamp: 4 },
	], options: {},
}, {
	name: "tool-result-image",
	messages: [
		{ role: "user", content: "shot", timestamp: 1 },
		{ role: "assistant", content: [{ type: "toolCall", id: "c2", name: "shoot", arguments: { z: "x" } }], stopReason: "toolUse", timestamp: 2 },
		{ role: "toolResult", toolCallId: "c2", toolName: "shoot", content: [{ type: "text", text: "here" }, { type: "image", data: "aW1n", mimeType: "image/jpeg" }], isError: true, timestamp: 3 },
	], options: {},
}, {
	name: "consecutive-user-and-system-removed",
	messages: [
		{ role: "system", content: "first", timestamp: 1 },
		{ role: "user", content: "one", timestamp: 2 },
		{ role: "user", content: "two", timestamp: 3 },
		{ role: "system", content: "second", toolsRemoved: [{ name: "old" }], timestamp: 4 },
		{ role: "user", content: "three", timestamp: 5 },
	], options: {},
}, {
	name: "max-tokens-default-and-off-reasoning",
	messages: [{ role: "user", content: "hello", timestamp: 1 }],
	options: { promptMode: "reasoning", reasoningEffort: "off", temperature: 0, topP: 1 },
}, {
	name: "assistant-text-next-to-tool-call",
	messages: [
		{ role: "user", content: "go", timestamp: 1 },
		{ role: "assistant", content: [{ type: "text", text: "before" }, { type: "toolCall", id: "c3", name: "lookup", arguments: { a: [1, { z: null }], z: true } }, { type: "text", text: "after" }], stopReason: "toolUse", timestamp: 2 },
		{ role: "toolResult", toolCallId: "c3", toolName: "lookup", content: [{ type: "text", text: "done" }], isError: false, timestamp: 3 },
	], options: {},
}, {
	name: "required-strict-schema-error", expectError: true,
	messages: [{ role: "system", content: "system", toolsAdded: [{ name: "lookup", parameters: { type: "object", $ref: "#/$defs/X" }, constrainedSampling: { type: "json_schema", strict: "require" } }], timestamp: 1 }, { role: "user", content: "hello", timestamp: 2 }], options: {},
}];
const model = { api: "mistral-conversations", id: "mistral-large-latest", provider: "mistral", baseUrl: "https://invalid.test",
	reasoning: true, maxTokens: 8192, contextWindow: 128000, input: ["text", "image"], cost: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0 } };
const fixtures = [];
for (const input of cases) {
	let payload, body, path;
	globalThis.fetch = async (url, init) => {
		path = new URL(url).pathname;
		body = init.body;
		return new Response('data: {"id":"r","choices":[{"delta":{"content":"ok"},"finish_reason":"stop"}]}\n\ndata: [DONE]\n\n', { headers: { "content-type": "text/event-stream" } });
	};
	const messages = input.messages.map(message => message.role === "assistant"
		? { ...message, api: model.api, provider: model.provider, model: model.id } : message);
	const result = await stream(model, { messages }, { ...input.options, apiKey: "token", env: {},
		toolChoice: input.forcedTool ? { type: "function", function: { name: input.forcedTool } } : input.options.toolChoice,
		onPayload(value) { payload = JSON.stringify(value); return input.replacement; },
	}).result();
	if (input.expectError) {
		if (!result.errorMessage || body !== undefined || payload !== undefined) throw new Error("strict schema did not fail before hooks/HTTP");
		fixtures.push({ ...input, messages, error: result.errorMessage });
		continue;
	}
	if (result.errorMessage || typeof body !== "string") throw new Error(result.errorMessage ?? "no request");
	fixtures.push({ ...input, messages, payload, path, body });
}
console.log(JSON.stringify(fixtures, null, 2));
