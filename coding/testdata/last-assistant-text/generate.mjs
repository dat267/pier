// Reference: pi v1.4.0 (4a080252), core/agent-session.ts getLastAssistantText.
// Regenerate from repository root:
// node coding/testdata/last-assistant-text/generate.mjs ./pi > coding/testdata/last-assistant-text/golden.json
// Execute the exact source method with a messages host, avoiding runtime boot
// and its optional generated artifacts. No implementation is copied here.
import { readFileSync } from "node:fs";
import { stripTypeScriptTypes } from "node:module";
import { join, resolve } from "node:path";

const root = resolve(process.argv[2] ?? "./pi");
const source = readFileSync(join(root, "packages/coding-agent/src/core/agent-session.ts"), "utf8");
const start = source.indexOf("\n\tgetLastAssistantText():");
const end = source.indexOf("\n\t// =", start);
if (start < 0 || end <= start) throw new Error("reference method markers changed");
const code = stripTypeScriptTypes(`class Probe {${source.slice(start, end)}\n}`);
const Probe = new Function(`${code}\nreturn Probe;`)();
const assistant = (text, extra = {}) => ({
	role: "assistant", api: "anthropic-messages", provider: "fixture", model: "m",
	content: [{ type: "text", text }], stopReason: "stop", timestamp: 1, ...extra,
});
const cases = [
	{ name: "no-assistant", messages: [{ role: "user", content: "hello", timestamp: 1 }] },
	{ name: "plain-trim", messages: [assistant(" \tanswer\r\n")] },
	{ name: "bom-boundaries", messages: [assistant("\ufeffanswer\ufeff")] },
	{ name: "nel-boundaries", messages: [assistant("\u0085answer\u0085")] },
	{ name: "bom-only", messages: [assistant("\ufeff")] },
	{ name: "text-blocks", messages: [assistant("", { content: [{ type: "text", text: " first" }, { type: "thinking", thinking: "hidden" }, { type: "text", text: " second " }] })] },
	{ name: "aborted-empty-skipped", messages: [assistant("previous"), assistant("", { stopReason: "aborted", content: [] })] },
	{ name: "aborted-with-text", messages: [assistant("previous"), assistant(" latest ", { stopReason: "aborted" })] },
	{ name: "latest-non-text", messages: [assistant("previous"), assistant("", { content: [{ type: "thinking", thinking: "hidden" }] })] },
];
const output = cases.map((test) => {
	const text = Probe.prototype.getLastAssistantText.call({ messages: test.messages });
	return { ...test, expectedText: text ?? "", rpcData: { text } };
});
console.log(JSON.stringify(output, null, 2));
