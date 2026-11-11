// Regenerate: node --experimental-strip-types ai/testdata/grammar-replay/generate.mjs ./pi > ai/testdata/grammar-replay/golden.json
import { registerHooks } from "node:module";
import { resolve } from "node:path";
import { pathToFileURL } from "node:url";
const root = resolve(process.argv[2]);
registerHooks({ resolve(s, c, next) {
	return s === "@earendil-works/pi-telemetry"
		? { url: pathToFileURL(resolve(root, "packages/telemetry/src/index.ts")).href, shortCircuit: true }
		: next(s, c);
} });
const fixtures = [];
const build = async (api, property, id) => {
	const { stream } = await import(pathToFileURL(resolve(root, `packages/ai/src/api/${api}.ts`)));
	{
		const model = { api, id: "gpt-5", provider: "openai", baseUrl: "https://api.openai.com/v1", reasoning: false,
			compat: { supportsOpenAIGrammarTools: true }, input: ["text"], maxTokens: 8192, contextWindow: 128000,
			cost: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0 } };
		const tool = { name: "expression", description: "Expression", parameters: { type: "object", properties: { [property]: { type: "string" } }, required: [property] },
			constrainedSampling: { type: "grammar", variants: { openai_regex: ".+" } } };
		const messages = [
			{ role: "system", content: "sys", toolsAdded: [tool], timestamp: 0 },
			{ role: "user", content: "an expression", timestamp: 1 },
			{ role: "assistant", api, provider: "openai", model: "gpt-5", content: [{ type: "toolCall", id, name: tool.name, arguments: { [property]: "x + 1" } }], stopReason: "toolUse", timestamp: 2 },
			{ role: "toolResult", toolCallId: id, toolName: tool.name, content: [{ type: "text", text: "2" }], isError: false, timestamp: 3 },
		];
		let payload;
		const result = await stream(model, { messages }, { apiKey: "sk-test", env: {}, onPayload(p) { payload = JSON.stringify(p); }, fetch: async () => new Response(
			api === "openai-responses"
				? 'data: {"type":"response.completed","response":{"id":"r1","status":"completed","output":[],"usage":{"input_tokens":0,"output_tokens":0,"total_tokens":0}}}\n\n'
				: 'data: {"id":"r1","choices":[{"delta":{"content":"ok"},"finish_reason":"stop"}]}\n\ndata: [DONE]\n\n',
			{ headers: { "content-type": "text/event-stream" } }),
		}).result();
		if (result.errorMessage || !payload) throw new Error(result.errorMessage ?? "no payload");
		return { model, messages, payload };
	}
};

for (const api of ["openai-responses", "openai-completions"]) {
	for (const property of ["payload", ""]) {
		const id = api === "openai-responses" ? "call_1|ctc_1" : "call_1";
		const built = await build(api, property, id);
		fixtures.push({ name: `${api}-${property || "empty-property"}`, ...built });
	}
}

// The replayed item id must match the type the call is replayed as: a call
// recorded with the other prefix loses its item id rather than sending one of
// the wrong type, and a third segment is dropped.
for (const { name, api, property, id } of [
	{ name: "openai-responses-function-prefix-on-custom-tool", api: "openai-responses", property: "payload", id: "call_1|fc_1" },
	{ name: "openai-responses-extra-segment", api: "openai-responses", property: "payload", id: "call_1|ctc_1|extra" },
	{ name: "openai-responses-no-item-id", api: "openai-responses", property: "payload", id: "call_1" },
	{ name: "openai-completions-pipe-id", api: "openai-completions", property: "payload", id: "call_1|ctc_1" },
]) {
	const built = await build(api, property, id);
	fixtures.push({ name, ...built });
}
console.log(JSON.stringify(fixtures, null, 2));
