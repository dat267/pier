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
for (const api of ["openai-responses", "openai-completions"]) {
	const { stream } = await import(pathToFileURL(resolve(root, `packages/ai/src/api/${api}.ts`)));
	for (const property of ["payload", ""]) {
		const model = { api, id: "gpt-5", provider: "openai", baseUrl: "https://api.openai.com/v1", reasoning: false,
			compat: { supportsOpenAIGrammarTools: true }, input: ["text"], maxTokens: 8192, contextWindow: 128000,
			cost: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0 } };
		const tool = { name: "expression", description: "Expression", parameters: { type: "object", properties: { [property]: { type: "string" } }, required: [property] },
			constrainedSampling: { type: "grammar", variants: { openai_regex: ".+" } } };
		const id = api === "openai-responses" ? "call_1|ctc_1" : "call_1";
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
		fixtures.push({ name: `${api}-${property || "empty-property"}`, model, messages, payload });
	}
}
console.log(JSON.stringify(fixtures, null, 2));
