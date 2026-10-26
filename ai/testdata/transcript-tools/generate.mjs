// Reference: pi v1.4.0 (4a080252), ai/src/utils/transcript.ts
// resolveTranscriptTools (and findLastNonAdditiveToolChange).
// Regenerate from the repository root:
// node --experimental-strip-types ai/testdata/transcript-tools/generate.mjs ./pi > ai/testdata/transcript-tools/golden.json
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
const { resolveTranscriptTools } = await import(pathToFileURL(resolve(root, "packages/ai/src/utils/transcript.ts")));

const tool = (name, description = `${name} tool`) => ({ name, description, parameters: { type: "object", properties: {} } });
const system = (extra = {}) => ({ role: "system", content: "", timestamp: 1, ...extra });
const user = (content = "hi") => ({ role: "user", content, timestamp: 2 });

const sequences = {
	"no-system": [user()],
	"initial-only": [system({ toolsAdded: [tool("read")] }), user()],
	"additive": [system({ toolsAdded: [tool("read")] }), user(), system({ toolsAdded: [tool("bash")] })],
	"removal": [system({ toolsAdded: [tool("read"), tool("bash")] }), user(), system({ toolsRemoved: [{ name: "read" }] })],
	"removal-then-addition": [system({ toolsAdded: [tool("read")] }), user(), system({ toolsRemoved: [{ name: "read" }] }), system({ toolsAdded: [tool("bash")] })],
	"redefinition": [system({ toolsAdded: [tool("read", "old")] }), user(), system({ toolsAdded: [tool("read", "new")] })],
	"removed-then-redeclared": [system({ toolsAdded: [tool("read")] }), system({ toolsRemoved: [{ name: "read" }] }), system({ toolsAdded: [tool("read")] })],
	"system-without-tools": [system(), user(), system({ content: "more" })],
	"tools-only-no-leading": [user(), system({ toolsAdded: [tool("grep")] })],
	"empty": [],
};
const supports = ["none", "additions", "changes"];
const fixtures = [];
for (const [name, messages] of Object.entries(sequences)) {
	for (const support of supports) {
		fixtures.push({ name: `${name}/${support}`, messages, support, expected: resolveTranscriptTools(messages, support) });
	}
}
console.log(JSON.stringify(fixtures, null, 2));
