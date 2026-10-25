// Reference: pi v1.4.0 (4a080252), packages/ai/src/api/google-generative-ai.ts
// and google-shared.ts stream transitions.
// Regenerate from the repository root:
// node --experimental-strip-types ai/testdata/google-stream/generate.mjs ./pi \
//   > ai/testdata/google-stream/golden.json
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
	pathToFileURL(resolve(root, "packages/ai/src/api/google-generative-ai.ts")).href
);

const sse = (chunks) => chunks.map((chunk) => `data: ${JSON.stringify(chunk)}\n\n`).join("");
const text = (value, extra = {}) => ({ candidates: [{ content: { role: "model", parts: [{ text: value, ...extra }] } }] });
const done = (finishReason, usageMetadata) => ({
	candidates: [{ content: { role: "model", parts: [{ text: "" }] }, finishReason }],
	...(usageMetadata ? { usageMetadata } : {}),
});

const scenarios = [
	{
		name: "text",
		chunks: [
			text("Hel"),
			{ candidates: [{ content: { role: "model", parts: [{ text: "lo" }] }, finishReason: "STOP" }] },
		],
	},
	{
		name: "thought",
		chunks: [
			text("plan", { thought: true, thoughtSignature: "c2ln" }),
			text(" more", { thought: true }),
			{ candidates: [{ content: { role: "model", parts: [{ text: "answer" }] }, finishReason: "STOP" }] },
		],
	},
	{
		name: "text-signature",
		chunks: [
			text("hello", { thoughtSignature: "c2ln" }),
			done("STOP"),
		],
	},
	{
		name: "function-call",
		chunks: [
			{
				candidates: [
					{
						content: {
							role: "model",
							parts: [{ functionCall: { name: "lookup", args: { z: 1, a: 2 } } }],
						},
						finishReason: "STOP",
					},
				],
			},
		],
	},
	{
		name: "thought-then-function-call",
		chunks: [
			text("think", { thought: true }),
			{
				candidates: [
					{
						content: {
							role: "model",
							parts: [
								{
									functionCall: { id: "call_1", name: "lookup", args: { path: "x" } },
									thoughtSignature: "c2ln",
								},
							],
						},
						finishReason: "STOP",
					},
				],
			},
		],
	},
	{
		name: "usage",
		chunks: [
			text("hi"),
			{
				candidates: [{ content: { role: "model", parts: [{ text: "" }] }, finishReason: "STOP" }],
				usageMetadata: {
					promptTokenCount: 30,
					candidatesTokenCount: 7,
					thoughtsTokenCount: 4,
					cachedContentTokenCount: 10,
					totalTokenCount: 41,
				},
			},
		],
	},
	{
		name: "max-tokens",
		chunks: [text("cut"), done("MAX_TOKENS")],
	},
	{
		name: "safety",
		chunks: [text("partial"), done("SAFETY")],
	},
	{
		name: "no-finish-reason",
		chunks: [text("hanging")],
	},
	{
		name: "usage-only-chunk",
		chunks: [
			text("hi"),
			{ usageMetadata: { promptTokenCount: 5, candidatesTokenCount: 1, totalTokenCount: 6 } },
			done("STOP"),
		],
	},
	{
		name: "empty-text-before-call",
		chunks: [
			text(""),
			{
				candidates: [
					{
						content: {
							role: "model",
							parts: [{ functionCall: { id: "call_1", name: "lookup", args: {} } }],
						},
						finishReason: "STOP",
					},
				],
			},
		],
	},
	{
		name: "empty-parts",
		chunks: [
			{ candidates: [{ content: { role: "model", parts: [] } }] },
			text("after-empty"),
			done("STOP"),
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

// Gemini omits function-call ids, so the reference synthesizes
// `<name>_<Date.now()>_<counter>` from a module-global counter; keep the shape
// and drop both process-dependent parts.
const normalizeId = (id) =>
	typeof id === "string" ? id.replace(/^(.+)_\d{10,}_\d+$/, "$1_<ts>_<n>") : id;

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
		out.id = normalizeId(block.id);
		out.name = block.name;
		out.arguments = canonical(block.arguments ?? {});
		if (block.thoughtSignature !== undefined) out.thoughtSignature = block.thoughtSignature;
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
			id: normalizeId(event.toolCall.id),
			name: event.toolCall.name,
			arguments: canonical(event.toolCall.arguments ?? {}),
		};
		if (event.toolCall.thoughtSignature !== undefined) out.toolCall.thoughtSignature = event.toolCall.thoughtSignature;
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
		api: "google-generative-ai",
		id: "gemini-3-pro",
		name: "Gemini 3 Pro",
		provider: "google",
		baseUrl: "https://invalid.test/v1beta",
		reasoning: true,
		input: ["text"],
		cost: { input: 2, output: 12, cacheRead: 0.5, cacheWrite: 0 },
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
