// Reference: pi v1.5.2 (b4afdddf), api/simple-options.ts clampMaxTokensToContext.
// Regenerate from the repository root:
// node --experimental-strip-types ai/testdata/clamp-max-tokens/generate.mjs ./pi > ai/testdata/clamp-max-tokens/golden.json
import { registerHooks } from "node:module";
import { resolve } from "node:path";
import { pathToFileURL } from "node:url";
const root = resolve(process.argv[2] ?? "./pi");
registerHooks({ resolve(specifier, context, next) {
	const map = {
		"@earendil-works/pi-telemetry": "packages/telemetry/src/index.ts",
		"@earendil-works/pi-schema": "packages/schema/src/index.ts",
		"@earendil-works/pi-schema/formats": "packages/schema/src/formats/index.ts",
		"@earendil-works/pi-agent-core": "packages/agent/src/index.ts",
	};
	return map[specifier] ? { url: pathToFileURL(resolve(root, map[specifier])).href, shortCircuit: true } : next(specifier, context);
} });
const { clampMaxTokensToContext } = await import(pathToFileURL(resolve(root, "packages/ai/src/api/simple-options.ts")).href);

// Reproducible transcripts: one user message of a fixed length, so the token
// estimate is stable and the Go side can build the same shape.
const transcripts = { empty: 0, small: 400, medium: 40000, large: 400000 };
// Context windows and requested caps around the two thresholds the formula
// turns on: the 15% input margin above the fixed 4096 tokens, and the 32768
// output floor below which only the fixed margin is kept.
const windows = [0, -1, 8192, 32768, 65536, 128000, 200000, 1000000, 2000000];
const caps = [1, 1024, 64000, 1000000];
const cases = [];
for (const [transcript, length] of Object.entries(transcripts)) {
	const context = { messages: length === 0 ? [] : [{ role: "user", content: "x".repeat(length), timestamp: 1 }] };
	for (const contextWindow of windows) for (const maxTokens of caps) {
		const model = { contextWindow, maxTokens: 64000 };
		cases.push({ transcript, contextWindow, maxTokens, want: clampMaxTokensToContext(model, context, maxTokens) });
	}
}
console.log(JSON.stringify(cases, null, 2));
