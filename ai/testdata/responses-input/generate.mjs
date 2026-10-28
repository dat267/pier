// Regenerate: node --experimental-strip-types ai/testdata/responses-input/generate.mjs ./pi > ai/testdata/responses-input/golden.json
import { registerHooks } from "node:module";
import { resolve } from "node:path";
import { pathToFileURL } from "node:url";
const root = resolve(process.argv[2]);
registerHooks({ resolve(s, c, next) {
	return s === "@earendil-works/pi-telemetry"
		? { url: pathToFileURL(resolve(root, "packages/telemetry/src/index.ts")).href, shortCircuit: true }
		: next(s, c);
} });
const { convertResponsesMessages } = await import(pathToFileURL(resolve(root, "packages/ai/src/api/openai-responses-shared.ts")));
const model = { api: "openai-responses", id: "gpt-5", provider: "openai", reasoning: true, input: ["text", "image"] };
const fixtures = [];
for (const { name, id = "call_1|fc_1", source = {}, namespace } of [
	{ name: "retained-item-id" },
	{ name: "retained-item-id-with-namespace", namespace: "tools" },
	{ name: "different-model-omits-item-id", source: { model: "gpt-5-mini" } },
	{ name: "no-item-id", id: "call_1" },
	{ name: "foreign-item-id", source: { api: "anthropic-messages", provider: "anthropic", model: "claude" } },
]) {
	const messages = [
		{ role: "user", content: "look up", timestamp: 1 },
		{ role: "assistant", api: model.api, provider: model.provider, model: model.id, ...source,
			content: [{ type: "toolCall", id, name: "lookup", arguments: { z: "value", a: 1 }, ...(namespace === undefined ? {} : { namespace }) }],
			stopReason: "toolUse", timestamp: 2 },
		{ role: "toolResult", toolName: "lookup", toolCallId: id, content: [{ type: "text", text: "found" }], isError: false, timestamp: 3 },
	];
	fixtures.push({ name, model, messages });
}
fixtures.push(
	{ name: "empty-transcript", model, messages: [] },
	{ name: "empty-user-blocks", model, messages: [{ role: "user", content: [], timestamp: 1 }] },
	{ name: "empty-system-content", model, messages: [{ role: "system", content: "", timestamp: 1 }] },
);
for (const fixture of fixtures) {
	fixture.input = JSON.stringify(convertResponsesMessages(fixture.model, { messages: fixture.messages }, new Set(["openai", "openai-codex", "opencode"])));
}
console.log(JSON.stringify(fixtures, null, 2));
