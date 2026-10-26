// Reference: pi v1.4.0 (4a080252), ai/src/utils/transcript.ts
// collapseSystemMessages / resolveTranscript.
// Regenerate from the repository root:
// node --experimental-strip-types ai/testdata/collapse-system/generate.mjs ./pi > ai/testdata/collapse-system/golden.json
import { registerHooks } from "node:module";
import { resolve } from "node:path";
import { pathToFileURL } from "node:url";
const root = resolve(process.argv[2] ?? "./pi");
registerHooks({ resolve(specifier, context, next) {
	const map = {
		"@earendil-works/pi-telemetry": "packages/telemetry/src/index.ts",
		"@earendil-works/pi-ai": "packages/ai/src/index.ts",
	};
	return map[specifier] ? { url: pathToFileURL(resolve(root, map[specifier])).href, shortCircuit: true } : next(specifier, context);
} });
const { collapseSystemMessages, resolveTranscript } = await import(pathToFileURL(resolve(root, "packages/ai/src/utils/transcript.ts")));

const tool = (name) => ({ name, description: `${name} tool`, parameters: { type: "object", properties: {} } });
const system = (extra = {}) => ({ role: "system", content: "", timestamp: 1, ...extra });
const user = (content) => ({ role: "user", content, timestamp: 2 });
const assistant = (text) => ({ role: "assistant", content: [{ type: "text", text }], api: "anthropic-messages", provider: "anthropic", model: "m", usage: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0, totalTokens: 0, cost: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0, total: 0 } }, stopReason: "stop", timestamp: 3 });

const cases = {
	"empty": [],
	"no-system": [user("hi"), assistant("there")],
	"leading-system": [system({ content: "base", toolsAdded: [tool("read")] }), user("hi")],
	"mid-system-merged": [system({ content: "base", timestamp: 1 }), user("hi"), system({ content: "more", timestamp: 5 })],
	"systems-only": [system({ content: "base", sections: { a: "A" }, timestamp: 1 }), system({ sections: { a: null }, timestamp: 2 })],
};
const fixtures = [];
for (const [name, messages] of Object.entries(cases)) {
	fixtures.push({ name: `${name}/collapse`, messages, function: "collapse", expected: collapseSystemMessages({ messages }).messages });
	fixtures.push({ name: `${name}/resolve-off`, messages, function: "resolve", supports: false, expected: resolveTranscript({ messages }, false).messages });
	fixtures.push({ name: `${name}/resolve-on`, messages, function: "resolve", supports: true, expected: resolveTranscript({ messages }, true).messages });
}
console.log(JSON.stringify(fixtures, null, 2));
