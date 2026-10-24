// Regenerate from the repository root (Node 24+):
// node --experimental-strip-types ai/testdata/google-wire/generate.mjs ./pi > ai/testdata/google-wire/golden.json
// Executes the pinned provider streams and their @google/genai SDK serializers.
import { registerHooks } from "node:module";
import { resolve } from "node:path";
import { pathToFileURL } from "node:url";

const root = resolve(process.argv[2]);
// The reference's built dist can lag its sources. Resolve the telemetry import
// to source too, without rebuilding or modifying the read-only checkout.
registerHooks({
	resolve(specifier, context, next) {
		if (specifier === "@earendil-works/pi-telemetry") {
			return { url: pathToFileURL(resolve(root, "packages/telemetry/src/index.ts")).href, shortCircuit: true };
		}
		return next(specifier, context);
	},
});
const providers = [];
for (const api of ["google-generative-ai", "google-vertex"]) {
	const { stream } = await import(pathToFileURL(resolve(root, `packages/ai/src/api/${api}.ts`)));
	providers.push({ api, stream });
}
const cases = [{
	name: "minimal",
	messages: [{ role: "user", content: "hello", timestamp: 1 }],
	options: {},
}, {
	name: "configured",
	messages: [{
		role: "system", content: "system", timestamp: 1,
		toolsAdded: [{
			name: "lookup", description: "Look up",
			parameters: { type: "object", properties: { z: { type: "string" }, a: { type: "number" } }, required: ["z", "a"] },
			constrainedSampling: { type: "json_schema", strict: "prefer" },
		}],
	}, { role: "user", content: "hello", timestamp: 2 }],
	options: { temperature: 0.5, maxTokens: 16, toolChoice: "any", thinking: { enabled: true, budgetTokens: 128 } },
}, {
	name: "tool-call-and-image",
	messages: [
		{ role: "user", content: [{ type: "image", data: "aW1hZ2U=", mimeType: "image/png" }], timestamp: 1 },
		{ role: "assistant", content: [{ type: "toolCall", id: "call-1", name: "lookup", arguments: { z: 1, a: 2 }, thoughtSignature: "c2ln" }], stopReason: "toolUse", timestamp: 2 },
		{ role: "toolResult", toolCallId: "call-1", toolName: "lookup", content: [{ type: "text", text: "found" }, { type: "image", data: "aW1hZ2U=", mimeType: "image/png" }], isError: false, timestamp: 3 },
	],
	options: {},
}, {
	name: "signed-empty-parts",
	messages: [
		{ role: "user", content: "hello", timestamp: 1 },
		{ role: "assistant", content: [{ type: "text", text: "", textSignature: "c2ln" }, { type: "thinking", thinking: "", thinkingSignature: "c2ln" }], stopReason: "stop", timestamp: 2 },
	],
	options: {},
}, {
	name: "null-tool-arguments",
	messages: [
		{ role: "user", content: "hello", timestamp: 1 },
		{ role: "assistant", content: [{ type: "toolCall", id: "call-1", name: "lookup", arguments: null }], stopReason: "toolUse", timestamp: 2 },
		{ role: "toolResult", toolCallId: "call-1", toolName: "lookup", content: [{ type: "text", text: "found" }], isError: false, timestamp: 3 },
	],
	options: {},
}];
const fixtures = [];
for (const { api, stream } of providers) {
	for (const input of cases) {
		const model = {
			api, id: "gemini-3-pro", provider: api === "google-vertex" ? "google-vertex" : "google",
			baseUrl: "https://invalid.test/v1", reasoning: true, maxTokens: 1000, contextWindow: 10000,
			cost: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0 }, input: ["text", "image"],
		};
		const messages = input.messages.map(message => message.role === "assistant"
			? { ...message, api, provider: model.provider, model: model.id } : message);
		let body;
		globalThis.fetch = async (_url, init) => {
			if (body !== undefined) throw new Error("unexpected second request");
			body = init.body;
			return new Response('data: {"candidates":[{"content":{"parts":[{"text":"ok"}],"role":"model"},"finishReason":"STOP"}]}\n\n',
				{ headers: { "content-type": "text/event-stream" } });
		};
		const result = await stream(model, { messages }, {
			...input.options, apiKey: "token", project: "project", location: "global", env: {},
		}).result();
		if (result.errorMessage || typeof body !== "string") throw new Error(result.errorMessage ?? "no body captured");
		fixtures.push({ api, ...input, messages, body });
	}
}
console.log(JSON.stringify(fixtures, null, 2));
