// Reference: pi v1.4.0 (4a080252), packages/coding-agent/src/core/model-resolver.ts
// resolveModelReference.
// Regenerate from the repository root:
// node --experimental-strip-types coding/testdata/model-resolve/generate.mjs ./pi > coding/testdata/model-resolve/golden.json
import { registerHooks } from "node:module";
import { resolve } from "node:path";
import { pathToFileURL } from "node:url";
const root = resolve(process.argv[2] ?? "./pi");
registerHooks({ resolve(specifier, context, next) {
	const map = {
		"@earendil-works/pi-telemetry": "packages/telemetry/src/index.ts",
		"@earendil-works/pi-ai": "packages/ai/src/index.ts",
		"@earendil-works/pi-agent-core": "packages/agent/src/index.ts",
	};
	return map[specifier] ? { url: pathToFileURL(resolve(root, map[specifier])).href, shortCircuit: true } : next(specifier, context);
} });
const { resolveModelReference } = await import(pathToFileURL(resolve(root, "packages/coding-agent/src/core/model-resolver.ts")));

const model = (provider, id, name = id, api = "anthropic-messages") => ({ provider, id, name, api });
const models = [
	model("anthropic", "claude-opus-4-5", "Claude Opus 4.5"),
	model("anthropic", "claude-sonnet-4-5", "Claude Sonnet 4.5"),
	model("openai", "gpt-5", "GPT-5"),
	model("openai", "gpt-5-mini", "GPT-5 mini"),
	model("zai", "glm-5", "GLM-5"),
	model("openrouter", "qwen/qwen3-coder:exacto", "Qwen3 Coder (exacto)"),
	model("google", "gemini-3-pro", "Gemini 3 Pro"),
	model("moonshot", "kimi-k2-thinking", "Kimi K2 Thinking"),
	model("custom", "xiaomi/mimo-v2.6-pro", "MiMo v2.6 Pro"),
	model("anthropic", "claude-haiku-4", "Claude Haiku 4"),
];
const usableAll = () => true;
const usableNoOpenAI = (provider) => provider !== "openai";
const vectors = [
	{ text: "claude-opus-4-5" },
	{ text: "claude-opus-4-5:high" },
	{ text: "claude-opus-4-5:bogus" },
	{ text: "claude-opus-4-5:bogus", thinkingSuffix: "strict" },
	{ text: "claude-opus-4-5:bogus", thinkingSuffix: "none" },
	{ text: "claude-opus-4-5:off" },
	{ text: "anthropic/claude-sonnet-4-5" },
	{ text: "ANTHROPIC/Claude-Sonnet-4-5" },
	{ text: "zai/glm-5" },
	{ text: "qwen/qwen3-coder:exacto" },
	{ text: "xiaomi/mimo-v2.6-pro" },
	{ text: "openrouter/qwen/qwen3-coder:exacto" },
	{ text: "gpt-5", provider: "openai" },
	{ text: "claude-opus-4-5", provider: "openai" },
	{ text: "claude-opus-4-5", provider: "OpenAI" },
	{ text: "gpt-5x", usable: "no-openai" },
	{ text: "sonnet" },
	{ text: "claud-opus" },
	{ text: "opus" },
	{ text: "kimi" },
	{ text: "" },
	{ text: "   " },
	{ text: " gemini-3-pro " },
	{ text: "unknown/model" },
	{ text: "unknown:high" },
	{ text: "claude-opus-4-5:high", fuzzy: false },
];
const project = (result) => {
	const out = { status: result.status };
	if (result.model) out.model = `${result.model.provider}/${result.model.id}`;
	if (result.thinkingLevel !== undefined) out.thinkingLevel = result.thinkingLevel;
	if (result.warning !== undefined) out.warning = result.warning;
	if (result.target) {
		out.target = {
			provider: result.target.provider,
			providerModels: result.target.providerModels.map((m) => `${m.provider}/${m.id}`),
			modelText: result.target.modelText,
			modelId: result.target.modelId,
			...(result.target.thinkingLevel !== undefined ? { thinkingLevel: result.target.thinkingLevel } : {}),
		};
	}
	return out;
};
const fixtures = vectors.map((vector) => {
	const request = {
		text: vector.text,
		...(vector.provider !== undefined ? { provider: vector.provider } : {}),
		thinkingSuffix: vector.thinkingSuffix ?? "lenient",
		fuzzy: vector.fuzzy ?? true,
	};
	const usable = vector.usable === "no-openai" ? usableNoOpenAI : usableAll;
	return { name: `${vector.text || "<empty>"}|${request.provider ?? "*"}|${request.thinkingSuffix}|fuzzy=${request.fuzzy}|${vector.usable ?? "all"}`, request, usable: vector.usable ?? "all", expected: project(resolveModelReference(request, models, usable)) };
});
console.log(JSON.stringify({ models: models.map((m) => ({ provider: m.provider, id: m.id, name: m.name, api: m.api })), fixtures }, null, 2));
