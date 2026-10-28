// Regenerate: node --experimental-strip-types ai/testdata/responses-grammar/generate.mjs ./pi > ai/testdata/responses-grammar/golden.json
import { registerHooks } from "node:module";
import { resolve } from "node:path";
import { pathToFileURL } from "node:url";
const root = resolve(process.argv[2]);
registerHooks({ resolve(s, c, next) {
	return s === "@earendil-works/pi-telemetry"
		? { url: pathToFileURL(resolve(root, "packages/telemetry/src/index.ts")).href, shortCircuit: true }
		: next(s, c);
} });
const { convertResponsesTools } = await import(pathToFileURL(resolve(root, "packages/ai/src/api/openai-responses-shared.ts")));
const tool = { name: "expression", description: "An expression", parameters: { type: "object", properties: { payload: { type: "string" } }, required: ["payload"] } };
const fixtures = [];
for (const { name, variants, options = { supportsOpenAIGrammarTools: true } } of [
	{ name: "regex", variants: { openai_regex: "[0-9]+" } },
	{ name: "lark", variants: { openai_lark: 'start: "hello"' } },
	{ name: "prefer-lark", variants: { openai_regex: "[0-9]+", openai_lark: 'start: "hello"' } },
	{ name: "deferred", variants: { openai_regex: "[0-9]+" }, options: { supportsOpenAIGrammarTools: true, toolSearchResult: true } },
	{ name: "unsupported-grammar", variants: { openai_regex: "[0-9]+" }, options: { supportsOpenAIGrammarTools: false } },
]) {
	const tools = [{ ...tool, constrainedSampling: { type: "grammar", variants } }];
	const resolvedOptions = { supportsStrictMode: false, ...options };
	fixtures.push({ name, tools, options: resolvedOptions, expected: JSON.stringify(convertResponsesTools(tools, resolvedOptions)) });
}
console.log(JSON.stringify(fixtures, null, 2));
