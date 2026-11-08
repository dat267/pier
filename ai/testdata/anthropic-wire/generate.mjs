// Regenerate from the repository root:
// node --experimental-strip-types ai/testdata/anthropic-wire/generate.mjs ./pi > ai/testdata/anthropic-wire/golden.json
// Reference: pi v1.5.0 (f9a26365), packages/ai/src/api/anthropic-messages.ts.
// Drives the pinned `stream` with an injected fetch and records the request it
// built: path, headers and body bytes. No network.
//
// Each case records two byte strings. `payload` is what onPayload sees, the
// built params with `betas` still in the body; `body` is what reaches the
// socket, after the SDK moves `betas` into the anthropic-beta header. Pier's
// BuildAnthropicParams must match `payload` and its splitAnthropicBetaParams
// must turn that into `body`.
import { registerHooks } from "node:module";
import { resolve } from "node:path";
import { pathToFileURL } from "node:url";

const root = resolve(process.argv[2] ?? "./pi");
registerHooks({
	resolve(specifier, context, next) {
		return specifier === "@earendil-works/pi-telemetry"
			? { url: pathToFileURL(resolve(root, "packages/telemetry/src/index.ts")).href, shortCircuit: true }
			: next(specifier, context);
	},
});
const { stream } = await import(pathToFileURL(resolve(root, "packages/ai/src/api/anthropic-messages.ts")));

const model = {
	api: "anthropic-messages",
	id: "claude-opus-4-5",
	name: "Claude Opus 4.5",
	provider: "anthropic",
	baseUrl: "https://api.anthropic.com",
	reasoning: true,
	input: ["text", "image"],
	cost: { input: 5, output: 25, cacheRead: 0.5, cacheWrite: 6.25 },
	contextWindow: 200000,
	maxTokens: 64000,
	compat: { supportsStrictTools: true },
};

const readTool = {
	name: "read_file",
	description: "Reads a file",
	parameters: { type: "object", properties: { path: { type: "string" } }, required: ["path"] },
};
const strictTool = {
	name: "emit",
	description: "Emit data",
	parameters: {
		type: "object",
		properties: { z: { type: "string" }, a: { type: "number" } },
		required: ["z", "a"],
		additionalProperties: false,
	},
	constrainedSampling: { type: "json_schema", strict: "prefer" },
};

const cases = [
	{
		name: "minimal",
		messages: [{ role: "user", content: "hi", timestamp: 1 }],
	},
	{
		name: "system-tools-cache",
		messages: [
			{ role: "system", content: "sys", toolsAdded: [readTool], timestamp: 0 },
			{ role: "user", content: [{ type: "text", text: "hi" }, { type: "image", data: "aW1hZ2U=", mimeType: "image/png" }], timestamp: 1 },
		],
	},
	{
		name: "thinking-budget",
		messages: [{ role: "user", content: "think", timestamp: 1 }],
		options: { thinkingEnabled: true, thinkingBudgetTokens: 2048, temperature: 0.9, maxTokens: 4096 },
	},
	{
		name: "assistant-replay",
		messages: [
			{ role: "user", content: "go", timestamp: 1 },
			{
				role: "assistant",
				timestamp: 2,
				stopReason: "toolUse",
				content: [
					{ type: "thinking", thinking: "plan", thinkingSignature: "c2ln" },
					{ type: "thinking", thinking: "[Reasoning redacted]", thinkingSignature: "cmVk", redacted: true },
					{ type: "text", text: "calling" },
					{ type: "toolCall", id: "toolu_1", name: "read_file", arguments: { path: "a.txt", nested: [1, { z: null }] } },
				],
			},
			{ role: "toolResult", toolCallId: "toolu_1", toolName: "read_file", isError: true, content: [{ type: "text", text: "missing" }], timestamp: 3 },
			{ role: "user", content: "again", timestamp: 4 },
		],
	},
	{
		name: "forced-tool",
		messages: [
			{ role: "system", content: "sys", toolsAdded: [readTool], timestamp: 0 },
			{ role: "user", content: "read", timestamp: 1 },
		],
		options: { toolChoice: { type: "tool", name: "read_file" }, temperature: 0 },
	},
	{
		name: "strict-schema",
		messages: [
			{ role: "system", content: "sys", toolsAdded: [strictTool], timestamp: 0 },
			{ role: "user", content: "emit", timestamp: 1 },
		],
	},
	{
		name: "adaptive-effort",
		compat: { forceAdaptiveThinking: true },
		messages: [{ role: "user", content: "effort", timestamp: 1 }],
		options: { thinkingEnabled: true, effort: "high" },
	},
	{
		name: "mid-convo-effort",
		compat: { supportsMidConvoEffort: true },
		messages: [
			{ role: "user", content: "one", timestamp: 1 },
			{ role: "assistant", content: [{ type: "text", text: "first" }], stopReason: "stop", timestamp: 2 },
			{ role: "user", content: "two", timestamp: 3 },
		],
		options: { thinkingEnabled: true, effort: "max" },
	},
	{
		name: "tool-result-image",
		messages: [
			{ role: "user", content: "shot", timestamp: 1 },
			{ role: "assistant", timestamp: 2, stopReason: "toolUse", content: [{ type: "toolCall", id: "toolu_2", name: "shoot", arguments: {} }] },
			{ role: "toolResult", toolCallId: "toolu_2", toolName: "shoot", isError: false, content: [{ type: "text", text: "here" }, { type: "image", data: "aW1n", mimeType: "image/jpeg" }], timestamp: 3 },
		],
	},
];

const fixtures = [];
for (const input of cases) {
	let body, path, headers, payload;
	globalThis.fetch = async (url, init) => {
		path = new URL(url).pathname;
		body = init.body;
		headers = Object.fromEntries(Object.entries(init.headers ?? {}));
		return new Response(
			'event: message_start\ndata: {"type":"message_start","message":{"id":"m","usage":{"input_tokens":1,"output_tokens":0}}}\n\n' +
				'event: content_block_start\ndata: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}\n\n' +
				'event: content_block_delta\ndata: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"ok"}}\n\n' +
				'event: content_block_stop\ndata: {"type":"content_block_stop","index":0}\n\n' +
				'event: message_delta\ndata: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":1}}\n\n' +
				'event: message_stop\ndata: {"type":"message_stop"}\n\n',
			{ headers: { "content-type": "text/event-stream" } },
		);
	};
	const modelForCase = input.compat ? { ...model, compat: { ...model.compat, ...input.compat } } : model;
	const messages = input.messages.map((message) =>
		message.role === "assistant" ? { ...message, api: model.api, provider: model.provider, model: model.id } : message,
	);
	const result = await stream(modelForCase, { messages }, {
		apiKey: "token", env: {}, ...input.options,
		onPayload(value) { payload = JSON.stringify(value); },
	}).result();
	if (result.errorMessage || typeof body !== "string") throw new Error(result.errorMessage ?? "no request");
	fixtures.push({ ...input, messages, path, headers, body, payload });
}
process.stdout.write(`${JSON.stringify(fixtures, null, "\t")}\n`);
