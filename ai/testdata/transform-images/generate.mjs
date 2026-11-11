// Regenerate: node --experimental-strip-types ai/testdata/transform-images/generate.mjs ./pi > ai/testdata/transform-images/golden.json
//
// Each case is a message set and a target model; `want` is the projection of
// pi v1.5.0 (f9a26365) api/transform-messages.ts `transformMessages` output:
// every message's role, its content blocks as {type, text} with the text of a
// placeholder block, and the tool result's id. That is the part a provider
// sees, so a dropped or kept image shows up.
import { resolve } from "node:path";
import { pathToFileURL } from "node:url";

const root = resolve(process.argv[2] ?? "./pi");
const { transformMessages } = await import(pathToFileURL(resolve(root, "packages/ai/src/api/transform-messages.ts")));

const image = (data) => ({ type: "image", data, mimeType: "image/png" });
const text = (value) => ({ type: "text", text: value });
const stamp = 1;
const cases = [
	{ name: "user-text-and-image", input: ["text"], messages: [{ role: "user", content: [text("look"), image("QQ==")], timestamp: stamp }] },
	{ name: "user-image-only", input: ["text"], messages: [{ role: "user", content: [image("QQ==")], timestamp: stamp }] },
	{ name: "user-multiple-images", input: ["text"], messages: [{ role: "user", content: [image("QQ=="), text("two"), image("REVG")], timestamp: stamp }] },
	{ name: "user-string-content", input: ["text"], messages: [{ role: "user", content: "plain", timestamp: stamp }] },
	{ name: "tool-result-image", input: ["text"], messages: [
		{ role: "user", content: "go", timestamp: stamp },
		{ role: "assistant", api: "openai-responses", provider: "openai", model: "gpt-5", content: [{ type: "toolCall", id: "call_1", name: "shot", arguments: {} }], stopReason: "toolUse", timestamp: stamp },
		{ role: "toolResult", toolCallId: "call_1", toolName: "shot", isError: false, content: [text("here"), image("QQ==")], timestamp: stamp },
	] },
	{ name: "image-capable-model", input: ["text", "image"], messages: [{ role: "user", content: [text("look"), image("QQ==")], timestamp: stamp }] },
	{ name: "empty-input-model", input: [], messages: [{ role: "user", content: [text("look"), image("QQ==")], timestamp: stamp }] },
	{ name: "system-message-with-image-block", input: ["text"], messages: [{ role: "system", content: "sys", timestamp: stamp }] },
	{ name: "assistant-text-only", input: ["text"], messages: [
		{ role: "user", content: "go", timestamp: stamp },
		{ role: "assistant", api: "openai-responses", provider: "openai", model: "gpt-5", content: [text("ok")], stopReason: "stop", timestamp: stamp },
	] },
];

const project = (messages) => messages.map((m) => ({
	role: m.role,
	...(m.toolCallId === undefined ? {} : { toolCallId: m.toolCallId }),
	content: typeof m.content === "string"
		? [{ type: "string", text: m.content }]
		: m.content.map((b) => (b.type === "text" ? { type: "text", text: b.text } : { type: b.type, text: b.text === undefined ? "" : b.text })),
}));

const out = cases.map((c) => {
	const model = { api: "openai-responses", id: "gpt-5", provider: "openai", input: c.input };
	return { name: c.name, model, messages: c.messages, want: JSON.stringify(project(transformMessages(c.messages, model))) };
});
console.log(JSON.stringify(out, null, 1));
