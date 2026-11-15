// Reference: pi v1.5.2 (b4afdddf) packages/ai/src/api/constrained-sampling.ts
// resolveGrammarConstrainedSampling and inferGrammarInputProperty. Run from the
// repository root:
//   node --experimental-strip-types ai/testdata/grammar-sampling/generate.mjs ./pi > ai/testdata/grammar-sampling/golden.json
// Each case records the resolved grammar, or the thrown message.
import { resolve } from "node:path";
import { pathToFileURL } from "node:url";

const root = resolve(process.argv[2] ?? "./pi");
const { resolveGrammarConstrainedSampling } = await import(
	pathToFileURL(resolve(root, "packages/ai/src/api/constrained-sampling.ts")).href
);

const grammar = (variants) => ({ type: "grammar", variants });
const lark = { openai_lark: "start: /.+/" };
const regex = { openai_regex: "^.+$" };

const tools = (parameters, config = grammar({ ...lark, ...regex })) => ({ name: "echo", parameters, constrainedSampling: config });
const objectSchema = { type: "object", properties: { q: { type: "string" } }, required: ["q"] };

const cases = [
	// Resolved variants.
	{ label: "both variants prefer lark", tool: tools(objectSchema, grammar({ ...lark, ...regex })) },
	{ label: "regex only", tool: tools(objectSchema, grammar(regex)) },
	{ label: "lark padded", tool: tools(objectSchema, grammar({ openai_lark: "  start: /.+/  " })) },
	{ label: "regex blank falls through", tool: tools(objectSchema, grammar({ openai_lark: "", openai_regex: "^.+$" })) },
	{ label: "no variants", tool: tools(objectSchema, grammar({ openai_lark: "  ", openai_regex: "" })) },
	{ label: "unsupported provider", tool: tools(objectSchema), supports: false },
	{ label: "json_schema config", tool: tools(objectSchema, { type: "json_schema", strict: "prefer" }) },
	{ label: "no config", tool: { name: "echo", parameters: objectSchema } },
	// Schema rejections.
	{ label: "two required", tool: tools({ type: "object", required: ["a", "b"], properties: { a: { type: "string" }, b: { type: "string" } } }) },
	{ label: "no required", tool: tools({ type: "object", properties: { q: { type: "string" } } }) },
	{ label: "non-string required member", tool: tools({ type: "object", required: ["q", 5], properties: { q: { type: "string" } } }) },
	{ label: "required not an array", tool: tools({ type: "object", required: "q", properties: { q: { type: "string" } } }) },
	{ label: "type not object", tool: tools({ type: "number", required: ["q"], properties: { q: { type: "string" } } }) },
	{ label: "boolean parameters", tool: tools(true) },
	{ label: "missing property entry", tool: tools({ type: "object", required: ["q"], properties: {} }) },
	{ label: "null property entry", tool: tools({ type: "object", required: ["q"], properties: { q: null } }) },
	{ label: "property not a string", tool: tools({ type: "object", required: ["q"], properties: { q: { type: "number" } } }) },
	{ label: "property type union", tool: tools({ type: "object", required: ["q"], properties: { q: { type: ["string"] } } }) },
];

const results = cases.map(({ label, tool, supports }) => {
	// The tool description travels with the case so the replay needs one file.
	const described = {
		parameters: tool.parameters,
		variants: tool.constrainedSampling?.variants ?? null,
		configType: tool.constrainedSampling?.type ?? null,
		noConfig: tool.constrainedSampling === undefined,
		supports: supports ?? true,
	};
	try {
		const resolved = resolveGrammarConstrainedSampling(tool, supports ?? true);
		return { label, tool: described, grammar: resolved ?? null };
	} catch (error) {
		return { label, tool: described, error: String(error?.message ?? error) };
	}
});
process.stdout.write(`${JSON.stringify(results, null, "\t")}\n`);
