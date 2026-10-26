// Reference: pi v1.4.0 (4a080252), packages/ai/src/api/anthropic-messages.ts
// stream event transitions.
// Regenerate from the repository root:
// node --experimental-strip-types ai/testdata/anthropic-stream/generate.mjs ./pi \
//   > ai/testdata/anthropic-stream/golden.json
import { registerHooks } from "node:module";
import { resolve } from "node:path";
import { pathToFileURL } from "node:url";

const root = resolve(process.argv[2] ?? "./pi");
// The workspace package is not linked; resolve it to the pinned source.
registerHooks({
	resolve(specifier, context, next) {
		return specifier === "@earendil-works/pi-telemetry"
			? { url: pathToFileURL(resolve(root, "packages/telemetry/src/index.ts")).href, shortCircuit: true }
			: next(specifier, context);
	},
});
const { stream } = await import(
	pathToFileURL(resolve(root, "packages/ai/src/api/anthropic-messages.ts")).href
);

const sse = (events) =>
	events.map((event) => `event: ${event.type}\ndata: ${JSON.stringify(event)}\n\n`).join("");

const scenarios = [
	{
		name: "text",
		events: [
			{
				type: "message_start",
				message: { id: "msg_1", model: "claude-sonnet-4-5", usage: { input_tokens: 10, output_tokens: 1 } },
			},
			{ type: "content_block_start", index: 0, content_block: { type: "text", text: "" } },
			{ type: "content_block_delta", index: 0, delta: { type: "text_delta", text: "Hel" } },
			{ type: "content_block_delta", index: 0, delta: { type: "text_delta", text: "lo" } },
			{ type: "content_block_stop", index: 0 },
			{ type: "message_delta", delta: { stop_reason: "end_turn" }, usage: { output_tokens: 5 } },
			{ type: "message_stop" },
		],
	},
	{
		name: "thinking",
		events: [
			{
				type: "message_start",
				message: { id: "msg_2", model: "claude-sonnet-4-5", usage: { input_tokens: 3, output_tokens: 1 } },
			},
			{ type: "content_block_start", index: 0, content_block: { type: "thinking", thinking: "" } },
			{ type: "content_block_delta", index: 0, delta: { type: "thinking_delta", thinking: "plan" } },
			{ type: "content_block_delta", index: 0, delta: { type: "signature_delta", signature: "sig" } },
			{ type: "content_block_stop", index: 0 },
			{ type: "content_block_start", index: 1, content_block: { type: "text", text: "" } },
			{ type: "content_block_delta", index: 1, delta: { type: "text_delta", text: "done" } },
			{ type: "content_block_stop", index: 1 },
			{ type: "message_delta", delta: { stop_reason: "end_turn" }, usage: { output_tokens: 7 } },
			{ type: "message_stop" },
		],
	},
	{
		name: "redacted-thinking",
		events: [
			{
				type: "message_start",
				message: { id: "msg_3", model: "claude-sonnet-4-5", usage: { input_tokens: 3, output_tokens: 0 } },
			},
			{
				type: "content_block_start",
				index: 0,
				content_block: { type: "redacted_thinking", data: "opaque" },
			},
			{ type: "content_block_stop", index: 0 },
			{ type: "message_delta", delta: { stop_reason: "end_turn" }, usage: { output_tokens: 0 } },
			{ type: "message_stop" },
		],
	},
	{
		name: "usage-full",
		events: [
			{
				type: "message_start",
				message: {
					id: "msg_u1", model: "claude-sonnet-4-5",
					usage: {
						input_tokens: 12, output_tokens: 1, cache_read_input_tokens: 7,
						cache_creation_input_tokens: 5, cache_creation: { ephemeral_1h_input_tokens: 4, ephemeral_5m_input_tokens: 1 },
					},
				},
			},
			{ type: "content_block_start", index: 0, content_block: { type: "text", text: "" } },
			{ type: "content_block_delta", index: 0, delta: { type: "text_delta", text: "hi" } },
			{ type: "content_block_stop", index: 0 },
			{ type: "message_delta", delta: { stop_reason: "end_turn" }, usage: { output_tokens: 6 } },
			{ type: "message_stop" },
		],
	},
	{
		name: "usage-thinking-tokens",
		events: [
			{
				type: "message_start",
				message: { id: "msg_u2", model: "claude-sonnet-4-5", usage: { input_tokens: 4, output_tokens: 1 } },
			},
			{ type: "content_block_start", index: 0, content_block: { type: "thinking", thinking: "" } },
			{ type: "content_block_delta", index: 0, delta: { type: "thinking_delta", thinking: "plan" } },
			{ type: "content_block_stop", index: 0 },
			{
				type: "message_delta", delta: { stop_reason: "end_turn" },
				usage: { output_tokens: 9, output_tokens_details: { thinking_tokens: 4 } },
			},
			{ type: "message_stop" },
		],
	},
	{
		name: "usage-delta-fields",
		events: [
			{
				type: "message_start",
				message: { id: "msg_u3", model: "claude-sonnet-4-5", usage: { input_tokens: 4, output_tokens: 0 } },
			},
			{ type: "content_block_start", index: 0, content_block: { type: "text", text: "" } },
			{ type: "content_block_delta", index: 0, delta: { type: "text_delta", text: "x" } },
			{ type: "content_block_stop", index: 0 },
			{
				type: "message_delta", delta: { stop_reason: "end_turn" },
				usage: {
					input_tokens: 4, output_tokens: 2, cache_read_input_tokens: 3,
					cache_creation_input_tokens: 2, cache_creation: { ephemeral_1h_input_tokens: 2 },
				},
			},
			{ type: "message_stop" },
		],
	},
	{
		name: "tool-use",
		events: [
			{
				type: "message_start",
				message: { id: "msg_4", model: "claude-sonnet-4-5", usage: { input_tokens: 9, output_tokens: 1 } },
			},
			{
				type: "content_block_start",
				index: 0,
				content_block: { type: "tool_use", id: "toolu_1", name: "lookup", input: {} },
			},
			{ type: "content_block_delta", index: 0, delta: { type: "input_json_delta", partial_json: '{"pat' } },
			{ type: "content_block_delta", index: 0, delta: { type: "input_json_delta", partial_json: 'h":"x"}' } },
			{ type: "content_block_stop", index: 0 },
			{ type: "message_delta", delta: { stop_reason: "tool_use" }, usage: { output_tokens: 4 } },
			{ type: "message_stop" },
		],
	},
	{
		name: "text-then-tool",
		events: [
			{
				type: "message_start",
				message: {
					id: "msg_5",
					model: "claude-sonnet-4-5",
					usage: { input_tokens: 5, output_tokens: 1, cache_read_input_tokens: 2, cache_creation_input_tokens: 3 },
				},
			},
			{ type: "content_block_start", index: 0, content_block: { type: "text", text: "" } },
			{ type: "content_block_delta", index: 0, delta: { type: "text_delta", text: "looking" } },
			{ type: "content_block_stop", index: 0 },
			{
				type: "content_block_start",
				index: 1,
				content_block: { type: "tool_use", id: "toolu_2", name: "read", input: {} },
			},
			{ type: "content_block_delta", index: 1, delta: { type: "input_json_delta", partial_json: '{"path":"a.txt"}' } },
			{ type: "content_block_stop", index: 1 },
			{ type: "message_delta", delta: { stop_reason: "tool_use" }, usage: { output_tokens: 6 } },
			{ type: "message_stop" },
		],
	},
	{
		name: "max-tokens",
		events: [
			{
				type: "message_start",
				message: { id: "msg_6", model: "claude-sonnet-4-5", usage: { input_tokens: 2, output_tokens: 1 } },
			},
			{ type: "content_block_start", index: 0, content_block: { type: "text", text: "" } },
			{ type: "content_block_delta", index: 0, delta: { type: "text_delta", text: "truncated" } },
			{ type: "content_block_stop", index: 0 },
			{ type: "message_delta", delta: { stop_reason: "max_tokens" }, usage: { output_tokens: 2 } },
			{ type: "message_stop" },
		],
	},
	{
		name: "stream-error",
		events: [
			{
				type: "message_start",
				message: { id: "msg_7", model: "claude-sonnet-4-5", usage: { input_tokens: 2, output_tokens: 1 } },
			},
			{ type: "content_block_start", index: 0, content_block: { type: "text", text: "" } },
			{ type: "content_block_delta", index: 0, delta: { type: "text_delta", text: "partial" } },
			{ type: "error", error: { type: "overloaded_error", message: "Overloaded" } },
		],
	},
	{
		name: "unterminated-block",
		events: [
			{
				type: "message_start",
				message: { id: "msg_8", model: "claude-sonnet-4-5", usage: { input_tokens: 2, output_tokens: 1 } },
			},
			{ type: "content_block_start", index: 0, content_block: { type: "text", text: "" } },
			{ type: "content_block_delta", index: 0, delta: { type: "text_delta", text: "hi" } },
			{ type: "message_stop" },
		],
	},
];

// `partial` is the documented live response-so-far helper rather than an
// event-time snapshot, so only the event's own scalars and the terminal message
// are comparable. Timestamps and durations are also nondeterministic.
const CONTENT_INDEX_TYPES = new Set([
	"text_start",
	"text_delta",
	"text_end",
	"thinking_start",
	"thinking_delta",
	"thinking_end",
	"toolcall_start",
	"toolcall_delta",
	"toolcall_end",
]);

function canonical(value) {
	if (Array.isArray(value)) return value.map(canonical);
	if (value && typeof value === "object") {
		const out = {};
		for (const key of Object.keys(value).sort()) out[key] = canonical(value[key]);
		return out;
	}
	return value;
}

function projectContent(block) {
	const out = { type: block.type };
	if (block.type === "text") out.text = block.text;
	if (block.type === "thinking") {
		out.thinking = block.thinking;
		out.thinkingSignature = block.thinkingSignature ?? "";
		if (block.redacted) out.redacted = true;
	}
	if (block.type === "toolCall") {
		out.id = block.id;
		out.name = block.name;
		out.arguments = canonical(block.arguments ?? {});
		if (typeof block.partialJson === "string") out.partialJson = block.partialJson;
	}
	return out;
}

function projectMessage(message) {
	const out = {
		content: message.content.map(projectContent),
		api: message.api,
		provider: message.provider,
		model: message.model,
		stopReason: message.stopReason,
		usage: canonical(message.usage),
	};
	for (const key of ["responseModel", "responseId", "rawStopReason", "errorMessage", "thinkingLevel", "providerThinkingLevel"]) {
		if (message[key] !== undefined) out[key] = message[key];
	}
	return out;
}

function project(event) {
	const out = { type: event.type };
	if (CONTENT_INDEX_TYPES.has(event.type)) out.contentIndex = event.contentIndex;
	if (event.delta !== undefined) out.delta = event.delta;
	if (event.content !== undefined) out.content = event.content;
	if (event.toolCall !== undefined) {
		out.toolCall = {
			id: event.toolCall.id,
			name: event.toolCall.name,
			arguments: canonical(event.toolCall.arguments ?? {}),
		};
	}
	if (event.reason !== undefined) out.reason = event.reason;
	if (event.message !== undefined) out.message = projectMessage(event.message);
	if (event.error !== undefined) out.error = projectMessage(event.error);
	return out;
}

const fixtures = [];
for (const scenario of scenarios) {
	const body = sse(scenario.events);
	globalThis.fetch = async () =>
		new Response(body, { status: 200, headers: { "content-type": "text/event-stream" } });
	const model = {
		api: "anthropic-messages",
		id: "claude-sonnet-4-5",
		name: "Claude Sonnet 4.5",
		provider: "anthropic",
		baseUrl: "https://invalid.test",
		reasoning: true,
		input: ["text"],
		cost: { input: 3, output: 15, cacheRead: 0.3, cacheWrite: 3.75 },
	};
	const events = [];
	const result = stream(
		model,
		{ messages: [{ role: "user", content: "hi", timestamp: 1 }] },
		{ apiKey: "test-key", env: {}, transport: "sse" },
	);
	for await (const event of result) events.push(project(event));
	fixtures.push({ name: scenario.name, sse: body, events });
}
console.log(JSON.stringify(fixtures, null, 2));
