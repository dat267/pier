// Reference: pi v1.4.0 (4a080252), packages/ai/src/api/openai-completions.ts
// stream event transitions.
// Regenerate from the repository root:
// node --experimental-strip-types ai/testdata/openai-stream/generate.mjs ./pi \
//   > ai/testdata/openai-stream/golden.json
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
	pathToFileURL(resolve(root, "packages/ai/src/api/openai-completions.ts")).href
);

const sse = (chunks) =>
	chunks.map((chunk) => `data: ${typeof chunk === "string" ? chunk : JSON.stringify(chunk)}\n\n`).join("");
const chunk = (delta, extra = {}) => ({
	id: "chatcmpl-1",
	object: "chat.completion.chunk",
	model: "gpt-5",
	choices: [{ index: 0, delta, finish_reason: null, ...extra }],
});

const scenarios = [
	{
		name: "text",
		chunks: [
			chunk({ role: "assistant", content: "" }),
			chunk({ content: "Hel" }),
			chunk({ content: "lo" }),
			{ choices: [{ index: 0, delta: {}, finish_reason: "stop" }] },
			"[DONE]",
		],
	},
	{
		name: "reasoning-then-text",
		chunks: [
			chunk({ reasoning_content: "plan " }),
			chunk({ reasoning_content: "more" }),
			chunk({ content: "answer" }),
			{ choices: [{ index: 0, delta: {}, finish_reason: "stop" }] },
			"[DONE]",
		],
	},
	{
		name: "tool-use",
		chunks: [
			{
				choices: [
					{
						index: 0,
						delta: {
							tool_calls: [
								{ index: 0, id: "call_1", type: "function", function: { name: "lookup", arguments: '{"pat' } },
							],
						},
					},
				],
			},
			{
				choices: [
					{ index: 0, delta: { tool_calls: [{ index: 0, function: { arguments: 'h":"x"}' } }] } },
				],
			},
			{ choices: [{ index: 0, delta: {}, finish_reason: "tool_calls" }] },
			"[DONE]",
		],
	},
	{
		name: "parallel-tools",
		chunks: [
			{
				choices: [
					{
						index: 0,
						delta: {
							tool_calls: [
								{ index: 0, id: "call_1", type: "function", function: { name: "read", arguments: '{"path":"a"}' } },
								{ index: 1, id: "call_2", type: "function", function: { name: "read", arguments: '{"path":"b"}' } },
							],
						},
					},
				],
			},
			{ choices: [{ index: 0, delta: {}, finish_reason: "tool_calls" }] },
			"[DONE]",
		],
	},
	{
		name: "usage",
		chunks: [
			chunk({ content: "hi" }),
			{ choices: [{ index: 0, delta: {}, finish_reason: "stop" }] },
			{
				id: "chatcmpl-1",
				object: "chat.completion.chunk",
				model: "gpt-5",
				choices: [],
				usage: {
					prompt_tokens: 11,
					completion_tokens: 7,
					total_tokens: 18,
					prompt_tokens_details: { cached_tokens: 3 },
					completion_tokens_details: { reasoning_tokens: 2 },
				},
			},
			"[DONE]",
		],
	},
	{
		name: "length",
		chunks: [
			chunk({ content: "truncated" }),
			{ choices: [{ index: 0, delta: {}, finish_reason: "length" }] },
			"[DONE]",
		],
	},
	{
		name: "no-finish-reason",
		chunks: [chunk({ content: "hanging" })],
	},
	{
		name: "error-chunk",
		chunks: [chunk({ content: "partial" }), { error: { message: "boom", type: "server_error" } }],
	},
	{
		name: "error-chunk-metadata",
		chunks: [
			chunk({ content: "partial" }),
			{ error: { message: "boom", metadata: { raw: "upstream-raw-detail" } } },
		],
	},
	{
		name: "error-chunk-string",
		chunks: [chunk({ content: "partial" }), { error: "plain failure" }],
	},
	{
		name: "error-chunk-object",
		chunks: [chunk({ content: "partial" }), { error: { type: "server_error" } }],
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
	}
	if (event.reason !== undefined) out.reason = event.reason;
	if (event.message !== undefined) out.message = projectMessage(event.message);
	if (event.error !== undefined) out.error = projectMessage(event.error);
	return out;
}

const fixtures = [];
for (const scenario of scenarios) {
	const body = sse(scenario.chunks);
	globalThis.fetch = async () =>
		new Response(body, { status: 200, headers: { "content-type": "text/event-stream" } });
	const model = {
		api: "openai-completions",
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
