// Reference: pi v1.4.0 (4a080252), ai/src/utils/text.ts and transcript.ts.
// Regenerate from the repository root:
// node --experimental-strip-types ai/testdata/system-prompt/generate.mjs ./pi > ai/testdata/system-prompt/golden.json
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
const text = await import(pathToFileURL(resolve(root, "packages/ai/src/utils/text.ts")));
const transcript = await import(pathToFileURL(resolve(root, "packages/ai/src/utils/transcript.ts")));

const tool = (name) => ({ name, description: `${name} tool`, parameters: { type: "object", properties: {} } });
const system = (content, extra = {}) => ({ role: "system", content, timestamp: 1, ...extra });
const user = (content) => ({ role: "user", content, timestamp: 2 });

const renderCases = {
	"text-string": system("base prompt"),
	"text-blocks": system([{ type: "text", text: "one" }, { type: "image", data: "aW1n", mimeType: "image/png" }, { type: "text", text: "two" }]),
	"text-sections": system("base", { sections: { alpha: "A", beta: "B" } }),
	"text-null-section": system("base", { sections: { alpha: "A", beta: null } }),
	"text-empty": system(""),
	"text-empty-and-empty-section": system("", { sections: { alpha: "", beta: "B" } }),
};
const updateCases = {
	"update-content-only": system("new instructions"),
	"update-section-set": system("", { sections: { alpha: "A" } }),
	"update-section-removed": system("", { sections: { alpha: null } }),
	"update-mixed": system("also this", { sections: { alpha: "A", beta: null } }),
	"update-empty": system(""),
};
const currentCases = {
	"current-single": [system("base", { timestamp: 10 })],
	"current-append": [system("base", { timestamp: 10 }), user("hi"), system("more", { timestamp: 20 })],
	"current-sections-patch": [system("base", { sections: { alpha: "A" }, timestamp: 10 }), system("", { sections: { alpha: "A2", beta: "B" }, timestamp: 20 })],
	"current-section-removed": [system("base", { sections: { alpha: "A", beta: "B" }, timestamp: 10 }), system("", { sections: { alpha: null }, timestamp: 20 })],
	"current-tools": [system("base", { toolsAdded: [tool("read")], timestamp: 10 }), system("", { toolsAdded: [tool("bash")], toolsRemoved: [{ name: "read" }], timestamp: 20 })],
	"current-tools-only": [user("hi"), system("", { toolsAdded: [tool("read")], timestamp: 30 })],
	"current-none": [user("hi")],
	"current-non-leading-system": [user("hi"), system("late", { timestamp: 40 })],
};
const fixtures = {
	render: Object.entries(renderCases).map(([name, message]) => ({ name, message, expected: text.getSystemMessageText(message) })),
	update: Object.entries(updateCases).map(([name, message]) => ({ name, message, expected: text.renderSystemMessageUpdate(message) })),
	current: Object.entries(currentCases).map(([name, messages]) => ({
		name,
		messages,
		expectedMessage: transcript.getCurrentSystemMessage(messages) ?? null,
		expectedPrompt: transcript.getCurrentSystemPrompt(messages),
		expectedTools: transcript.getCurrentTools(messages),
	})),
};
console.log(JSON.stringify(fixtures, null, 2));
