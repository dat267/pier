// Reference: pi v1.4.0 (4a080252), api/simple-options.ts buildBaseOptions and
// provider streamSimple wrappers. No provider network calls are made.
// Regenerate: node --experimental-strip-types ai/testdata/simple-max-tokens/generate.mjs ./pi > ai/testdata/simple-max-tokens/golden.json
import { registerHooks } from "node:module";
import { resolve } from "node:path";
import { pathToFileURL } from "node:url";
const root = resolve(process.argv[2] ?? "./pi");
registerHooks({ resolve(specifier, context, next) {
	return specifier === "@earendil-works/pi-telemetry"
		? { url: pathToFileURL(resolve(root, "packages/telemetry/src/index.ts")).href, shortCircuit: true }
		: next(specifier, context);
} });
const providers = [
	["anthropic-messages", "anthropic", "claude-sonnet-4-5"],
	["bedrock-converse-stream", "amazon-bedrock", "anthropic.claude-3-7-sonnet-20250219-v1:0"],
	["google-generative-ai", "google", "gemini-2.5-flash"],
	["google-vertex", "google-vertex", "gemini-2.5-flash"],
	["mistral-conversations", "mistral", "devstral-small-2507"],
	["openai-completions", "openai", "gpt-4o"],
	["openai-responses", "openai", "gpt-4o"],
	["azure-openai-responses", "azure", "gpt-4o"],
];
const fixtures = [];
for (const [api, provider, id] of providers) {
	const module = await import(pathToFileURL(resolve(root, `packages/ai/src/api/${api}.ts`)));
	const scenarios = [
		{ name: "default-context-cap", options: {}, contextWindow: 5000 },
		{ name: "explicit-context-cap", options: { maxTokens: 10000 }, contextWindow: 5000 },
		{ name: "explicit-small-cap", options: { maxTokens: 20 }, contextWindow: 5000 },
		{ name: "default-model-cap", options: {}, contextWindow: 128000 },
		{ name: "raw-stream-control", options: {}, contextWindow: 5000, simple: false },
	];
	if (api === "anthropic-messages" || api === "bedrock-converse-stream") {
		scenarios.push({ name: "budget-explicit-cap", options: { maxTokens: 2000, reasoning: "low" }, contextWindow: 128000, reasoning: true });
		scenarios.push({ name: "budget-context-cap", options: { maxTokens: 2000, reasoning: "low" }, contextWindow: 5000, reasoning: true });
	}
	for (const scenario of scenarios) {
		const model = { api, provider, id, name: id, baseUrl: "https://invalid.test", reasoning: scenario.reasoning ?? false, input: ["text"], contextWindow: scenario.contextWindow, maxTokens: 8192, cost: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0 } };
		let captured = false, maxTokens;
		globalThis.fetch = async () => { throw new Error("unexpected network call"); };
		const result = await (scenario.simple === false ? module.stream : module.streamSimple)(model, { messages: [{ role: "user", content: "hi", timestamp: 1 }] }, {
			...scenario.options, apiKey: "test-key", env: { AWS_REGION: "us-east-1", AWS_BEARER_TOKEN_BEDROCK: "test-key" },
			onPayload(payload) {
				captured = true;
				maxTokens = payload.max_tokens ?? payload.max_completion_tokens ?? payload.max_output_tokens ?? payload.maxTokens ?? payload.inferenceConfig?.maxTokens ?? payload.config?.maxOutputTokens;
				throw new Error("capture complete");
			},
		}).result();
		if (!captured || result.errorMessage !== "capture complete") throw new Error(`${api}/${scenario.name}: ${result.errorMessage}`);
		fixtures.push({ name: `${api}/${scenario.name}`, model, options: scenario.options, simple: scenario.simple !== false, expectedMaxTokens: maxTokens ?? null });
	}
}
console.log(JSON.stringify(fixtures, null, 2));
