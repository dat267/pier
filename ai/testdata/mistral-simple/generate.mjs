// Reference: pi v1.4.0 (4a080252), api/mistral-conversations.ts streamSimple.
// Regenerate: node --experimental-strip-types ai/testdata/mistral-simple/generate.mjs ./pi > ai/testdata/mistral-simple/golden.json
import { registerHooks } from "node:module";
import { resolve } from "node:path";
import { pathToFileURL } from "node:url";
const root = resolve(process.argv[2] ?? "./pi");
registerHooks({ resolve(specifier, context, next) {
	return specifier === "@earendil-works/pi-telemetry"
		? { url: pathToFileURL(resolve(root, "packages/telemetry/src/index.ts")).href, shortCircuit: true }
		: next(specifier, context);
} });
const { streamSimple } = await import(pathToFileURL(resolve(root, "packages/ai/src/api/mistral-conversations.ts")));
const base = { api: "mistral-conversations", provider: "mistral", baseUrl: "https://invalid.test", reasoning: true, input: ["text"], contextWindow: 128000, maxTokens: 8192, cost: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0 } };
const effortMap = { off: "none", minimal: null, low: null, medium: null, high: "high", xhigh: null, max: null };
const scenarios = [
	{ name: "small-off", model: { id: "mistral-small-2603", thinkingLevelMap: effortMap }, reasoning: "off" },
	{ name: "small-unspecified", model: { id: "mistral-small-2603", thinkingLevelMap: effortMap } },
	{ name: "small-high", model: { id: "mistral-small-2603", thinkingLevelMap: effortMap }, reasoning: "high" },
	{ name: "large-4-high", model: { id: "mistral-large-4", thinkingLevelMap: effortMap }, reasoning: "high" },
	{ name: "glm-5-3-low", model: { id: "zai-glm-5-3", thinkingLevelMap: { off: null, minimal: null, low: "low", medium: null, high: "high", xhigh: null, max: "max" } }, reasoning: "low" },
	{ name: "custom-map", model: { id: "custom", thinkingLevelMap: { high: "max" } }, reasoning: "high" },
	{ name: "no-map-reasoning", model: { id: "custom" }, reasoning: "high" },
	{ name: "small-no-map", model: { id: "mistral-small-latest" }, reasoning: "high" },
	{ name: "nonreasoning", model: { id: "custom", reasoning: false, thinkingLevelMap: effortMap }, reasoning: "high" },
];
const fixtures = [];
for (const scenario of scenarios) {
	globalThis.fetch = async () => new Response('data: {"choices":[{"delta":{"content":"ok"},"finish_reason":"stop"}]}\n\ndata: [DONE]\n\n', { headers: { "content-type": "text/event-stream" } });
	let observed;
	const result = await streamSimple({ ...base, ...scenario.model }, { messages: [{ role: "user", content: "hi", timestamp: 1 }] }, {
		apiKey: "test-key", env: {}, reasoning: scenario.reasoning,
		onPayload(payload) { observed = { ...(payload.reasoningEffort !== undefined && { reasoningEffort: payload.reasoningEffort }), ...(payload.promptMode !== undefined && { promptMode: payload.promptMode }) }; },
	}).result();
	if (result.errorMessage || !observed) throw new Error(result.errorMessage ?? "no payload");
	fixtures.push({ ...scenario, model: { ...base, ...scenario.model }, expected: observed });
}
console.log(JSON.stringify(fixtures, null, 2));
