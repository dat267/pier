// Reference: pi v1.4.0 (4a080252),
// packages/ai/src/api/openai-responses.ts and openai-responses-shared.ts
// (processResponsesStream).
// Regenerate from the repository root:
// node --experimental-strip-types ai/testdata/openai-responses-stream/generate.mjs ./pi \
//   > ai/testdata/openai-responses-stream/golden.json
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
	pathToFileURL(resolve(root, "packages/ai/src/api/openai-responses.ts")).href
);

const sse = (events) =>
	events
		.map((event) => `event: ${event.type}\ndata: ${JSON.stringify(event)}\n\n`)
		.join("");
const created = { type: "response.created", response: { id: "resp_1", status: "in_progress" } };
const messageItem = (text) => ({
	type: "message",
	id: "msg_1",
	role: "assistant",
	content: [{ type: "output_text", text }],
});
const reasoningItem = (summary) => ({
	type: "reasoning",
	id: "rs_1",
	summary: summary.map((text) => ({ type: "summary_text", text })),
});
const functionItem = (args) => ({
	type: "function_call",
	id: "fc_1",
	call_id: "call_1",
	name: "lookup",
	arguments: args,
	status: "completed",
});

const scenarios = [
	{
		name: "text",
		events: [
			created,
			{ type: "response.output_item.added", output_index: 0, item: { ...messageItem(""), content: [] } },
			{ type: "response.output_text.delta", output_index: 0, item_id: "msg_1", delta: "Hel" },
			{ type: "response.output_text.delta", output_index: 0, item_id: "msg_1", delta: "lo" },
			{ type: "response.output_item.done", output_index: 0, item: messageItem("Hello") },
			{ type: "response.completed", response: { id: "resp_1", status: "completed" } },
		],
	},
	{
		name: "reasoning",
		events: [
			created,
			{ type: "response.output_item.added", output_index: 0, item: { ...reasoningItem([]), summary: [] } },
			{ type: "response.reasoning_summary_text.delta", output_index: 0, item_id: "rs_1", delta: "plan" },
			{ type: "response.reasoning_summary_part.done", output_index: 0, item_id: "rs_1" },
			{ type: "response.reasoning_summary_text.delta", output_index: 0, item_id: "rs_1", delta: "next" },
			{ type: "response.output_item.done", output_index: 0, item: reasoningItem(["plan", "next"]) },
			{ type: "response.completed", response: { id: "resp_1", status: "completed" } },
		],
	},
	{
		name: "function-call",
		events: [
			created,
			{
				type: "response.output_item.added",
				output_index: 0,
				item: { ...functionItem(""), arguments: "" },
			},
			{ type: "response.function_call_arguments.delta", output_index: 0, item_id: "fc_1", delta: '{"pat' },
			{ type: "response.function_call_arguments.delta", output_index: 0, item_id: "fc_1", delta: 'h":"x"}' },
			{ type: "response.output_item.done", output_index: 0, item: functionItem('{"path":"x"}') },
			{ type: "response.completed", response: { id: "resp_1", status: "completed" } },
		],
	},
	{
		name: "usage",
		events: [
			created,
			{ type: "response.output_item.added", output_index: 0, item: { ...messageItem(""), content: [] } },
			{ type: "response.output_text.delta", output_index: 0, item_id: "msg_1", delta: "hi" },
			{ type: "response.output_item.done", output_index: 0, item: messageItem("hi") },
			{
				type: "response.completed",
				response: {
					id: "resp_1",
					status: "completed",
					usage: {
						input_tokens: 20,
						output_tokens: 9,
						total_tokens: 29,
						input_tokens_details: { cached_tokens: 5, cache_write_tokens: 2 },
						output_tokens_details: { reasoning_tokens: 3 },
					},
				},
			},
		],
	},
	{
		name: "incomplete-max-tokens",
		events: [
			created,
			{ type: "response.output_item.added", output_index: 0, item: { ...messageItem(""), content: [] } },
			{ type: "response.output_text.delta", output_index: 0, item_id: "msg_1", delta: "cut" },
			{ type: "response.output_item.done", output_index: 0, item: messageItem("cut") },
			{
				type: "response.incomplete",
				response: {
					id: "resp_1",
					status: "incomplete",
					incomplete_details: { reason: "max_output_tokens" },
				},
			},
		],
	},
	{
		name: "failed",
		events: [
			created,
			{ type: "response.output_item.added", output_index: 0, item: { ...messageItem(""), content: [] } },
			{ type: "response.output_text.delta", output_index: 0, item_id: "msg_1", delta: "partial" },
			{
				type: "response.failed",
				response: {
					id: "resp_1",
					status: "failed",
					error: { code: "server_error", message: "boom" },
				},
			},
		],
	},
	{
		name: "error-event",
		events: [
			created,
			{ type: "response.output_item.added", output_index: 0, item: { ...messageItem(""), content: [] } },
			{ type: "response.output_text.delta", output_index: 0, item_id: "msg_1", delta: "partial" },
			{ type: "error", code: "server_error", message: "gateway down" },
		],
	},
	{
		name: "no-terminal",
		events: [
			created,
			{ type: "response.output_item.added", output_index: 0, item: { ...messageItem(""), content: [] } },
			{ type: "response.output_text.delta", output_index: 0, item_id: "msg_1", delta: "hanging" },
		],
	},
	{
		name: "final-answer-phase",
		events: [
			created,
			{ type: "response.output_item.added", output_index: 0, item: { ...messageItem(""), content: [] } },
			{ type: "response.output_text.delta", output_index: 0, item_id: "msg_1", delta: "done" },
			{
				type: "response.output_item.done",
				output_index: 0,
				item: { ...messageItem("done"), phase: "final_answer" },
			},
			{ type: "response.completed", response: { id: "resp_1", status: "completed" } },
		],
	},
	{
		name: "done-without-added",
		events: [
			created,
			{ type: "response.output_item.done", output_index: 0, item: messageItem("direct") },
			{ type: "response.completed", response: { id: "resp_1", status: "completed" } },
		],
	},
	{
		name: "reasoning-text-and-encrypted-backfill",
		events: [
			created,
			{
				type: "response.output_item.added",
				output_index: 0,
				item: { ...reasoningItem([]), summary: [] },
			},
			{ type: "response.reasoning_text.delta", output_index: 0, item_id: "rs_1", delta: "raw " },
			{
				type: "response.output_item.done",
				output_index: 0,
				item: { ...reasoningItem([]), content: [{ type: "reasoning_text", text: "raw text" }] },
			},
			{
				type: "response.completed",
				response: {
					id: "resp_1",
					status: "completed",
					output: [
						{ type: "reasoning", id: "rs_1", encrypted_content: "enc-blob" },
					],
				},
			},
		],
	},
	{
		name: "refusal",
		events: [
			created,
			{ type: "response.output_item.added", output_index: 0, item: { ...messageItem(""), content: [] } },
			{ type: "response.refusal.delta", output_index: 0, item_id: "msg_1", delta: "no thanks" },
			{
				type: "response.output_item.done",
				output_index: 0,
				item: {
					type: "message",
					id: "msg_1",
					role: "assistant",
					content: [{ type: "refusal", refusal: "no thanks" }],
				},
			},
			{ type: "response.completed", response: { id: "resp_1", status: "completed" } },
		],
	},
	{
		name: "unfinished-tool-call",
		events: [
			created,
			{
				type: "response.output_item.added",
				output_index: 0,
				item: { ...functionItem(""), arguments: "" },
			},
			{ type: "response.function_call_arguments.delta", output_index: 0, item_id: "fc_1", delta: '{"path"' },
			{ type: "response.completed", response: { id: "resp_1", status: "completed" } },
		],
	},
	{
		name: "incomplete-content-filter",
		events: [
			created,
			{ type: "response.output_item.added", output_index: 0, item: { ...messageItem(""), content: [] } },
			{ type: "response.output_text.delta", output_index: 0, item_id: "msg_1", delta: "cut" },
			{ type: "response.output_item.done", output_index: 0, item: messageItem("cut") },
			{
				type: "response.incomplete",
				response: {
					id: "resp_1",
					status: "incomplete",
					incomplete_details: { reason: "content_filter" },
				},
			},
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
	if (block.type === "text") {
		out.text = block.text;
		if (block.textSignature !== undefined) out.textSignature = block.textSignature;
	}
	if (block.type === "thinking") {
		out.thinking = block.thinking;
		out.thinkingSignature = block.thinkingSignature ?? "";
		if (block.redacted) out.redacted = true;
	}
	if (block.type === "toolCall") {
		out.id = block.id;
		out.name = block.name;
		out.arguments = canonical(block.arguments ?? {});
		if (block.namespace !== undefined) out.namespace = block.namespace;
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
	for (const key of [
		"responseModel",
		"responseId",
		"rawStopReason",
		"errorMessage",
		"thinkingLevel",
		"providerThinkingLevel",
	]) {
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
		if (event.toolCall.namespace !== undefined) out.toolCall.namespace = event.toolCall.namespace;
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
		api: "openai-responses",
		id: "gpt-5",
		name: "GPT-5",
		provider: "openai",
		baseUrl: "https://invalid.test/v1",
		reasoning: true,
		input: ["text"],
		cost: { input: 1.25, output: 10, cacheRead: 0.125, cacheWrite: 0 },
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
