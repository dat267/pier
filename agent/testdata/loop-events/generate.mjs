// Reference: pi v1.6.0 (faac5858), packages/agent/src/agent-loop.ts
// runAgentLoop. Each scenario drives the reference loop with a scripted stream
// function and plain tools, and records the event sequence it emits plus the
// messages it appends, so the port's loop is compared against the reference
// instead of against expectations written by hand.
//
// Timestamps, durations and costs are timing artifacts and are dropped; every
// other field of an emitted event is kept.
//
// Regenerate from the repository root:
// node --experimental-strip-types agent/testdata/loop-events/generate.mjs ./pi > agent/testdata/loop-events/golden.json
import { resolve } from "node:path";
import { registerHooks } from "node:module";
import { pathToFileURL } from "node:url";

const root = resolve(process.argv[2] ?? "./pi");
registerHooks({
	resolve(specifier, context, next) {
		const map = {
			"@earendil-works/pi-ai": "packages/ai/src/index.ts",
			"@earendil-works/pi-agent-core": "packages/agent/src/index.ts",
			"@earendil-works/pi-telemetry": "packages/telemetry/src/index.ts",
		};
		return map[specifier] ? { url: pathToFileURL(resolve(root, map[specifier])).href, shortCircuit: true } : next(specifier, context);
	},
});
const { runAgentLoop } = await import(pathToFileURL(resolve(root, "packages/agent/src/agent-loop.ts")).href);
const { createAssistantMessageEventStream } = await import(
	pathToFileURL(resolve(root, "packages/ai/src/utils/event-stream.ts")).href
);

const USAGE = { input: 0, output: 0, cacheRead: 0, cacheWrite: 0, totalTokens: 0, cost: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0, total: 0 } };
const model = { api: "test", provider: "test", id: "m" };
const assistant = (content, stopReason) => ({
	role: "assistant",
	content,
	api: model.api,
	provider: model.provider,
	model: model.id,
	usage: USAGE,
	stopReason,
	timestamp: 1,
});

const projectContent = (content) =>
	(typeof content === "string" ? [{ type: "text", text: content }] : (content ?? [])).map((block) => {
		if (block.type === "text") return { type: "text", text: block.text };
		if (block.type === "toolCall") return { type: "toolCall", id: block.id, name: block.name, arguments: block.arguments };
		return { type: block.type };
	});

const projectMessage = (message) => {
	if (!message) return null;
	const out = { role: message.role, content: projectContent(message.content) };
	if (message.stopReason !== undefined) out.stopReason = message.stopReason;
	if (message.errorMessage !== undefined) out.errorMessage = message.errorMessage;
	if (message.toolCallId !== undefined) out.toolCallId = message.toolCallId;
	if (message.toolName !== undefined) out.toolName = message.toolName;
	if (message.isError !== undefined) out.isError = message.isError;
	if (message.toolsAdded !== undefined) out.toolsAdded = message.toolsAdded.map((tool) => tool.name);
	if (message.toolsRemoved !== undefined) out.toolsRemoved = message.toolsRemoved.map((tool) => tool.name);
	return out;
};

const projectEvent = (event) => {
	const out = { type: event.type };
	switch (event.type) {
		case "message_start":
		case "message_end":
			out.message = projectMessage(event.message);
			break;
		case "message_update":
			out.update = event.assistantMessageEvent.type;
			if (event.assistantMessageEvent.delta !== undefined) out.delta = event.assistantMessageEvent.delta;
			out.message = projectMessage(event.message);
			break;
		case "tool_execution_start":
			out.toolCallId = event.toolCallId;
			out.toolName = event.toolName;
			out.args = event.args;
			break;
		case "tool_execution_end":
			out.toolCallId = event.toolCallId;
			out.toolName = event.toolName;
			out.isError = event.isError;
			out.content = projectContent(event.result.content);
			out.details = event.result.details ?? null;
			out.terminate = event.result.terminate === true;
			out.hasDuration = event.durationMs !== undefined;
			break;
		case "turn_end":
			out.message = projectMessage(event.message);
			out.toolResults = event.toolResults.map(projectMessage);
			break;
		case "agent_end":
			out.messages = event.messages.map(projectMessage);
			break;
	}
	return out;
};

const tools = (spec) => {
	const built = {};
	for (const tool of spec) {
		built[tool.name] = {
			name: tool.name,
			label: tool.name,
			description: tool.name,
			parameters: { type: "object", properties: { v: { type: "number" } } },
			...(tool.mode ? { executionMode: tool.mode } : {}),
			async execute(_id, args) {
				if (tool.behavior === "boom") throw new Error("boom");
				const terminate = tool.behavior === "stop";
				return { content: [{ type: "text", text: `${tool.name}:${args.v ?? 0}` }], details: { by: tool.name }, ...(terminate ? { terminate: true } : {}) };
			},
		};
	}
	return built;
};

const scriptedStream = (scenario) => {
	let call = 0;
	return () => {
		const turn = scenario.turns[call++] ?? { text: "fallback", stopReason: "stop" };
		const stream = createAssistantMessageEventStream();
		const partial = assistant([], turn.stopReason);
		stream.push({ type: "start", partial });
		let text = "";
		for (const chunk of turn.chunks ?? (turn.text ? [turn.text] : [])) {
			text += chunk;
			stream.push({ type: "text_delta", contentIndex: 0, delta: chunk, partial: assistant([{ type: "text", text }], turn.stopReason) });
		}
		const content = [];
		if (text) content.push({ type: "text", text });
		for (const call_ of turn.toolCalls ?? []) content.push({ type: "toolCall", id: call_.id, name: call_.name, arguments: call_.args });
		const final = assistant(content, turn.stopReason);
		stream.push({ type: "done", reason: turn.stopReason, message: final });
		stream.end(final);
		return stream;
	};
};

const scenarios = [
	{
		name: "text-turn",
		prompt: "hi",
		tools: [],
		turns: [{ chunks: ["he", "llo"], stopReason: "stop" }],
	},
	{
		name: "parallel-batch",
		prompt: "run",
		tools: [{ name: "a" }, { name: "b" }],
		turns: [
			{ toolCalls: [{ id: "c1", name: "a", args: { v: 1 } }, { id: "c2", name: "b", args: { v: 2 } }], stopReason: "toolUse" },
			{ text: "done", stopReason: "stop" },
		],
	},
	{
		name: "sequential-batch",
		prompt: "run",
		tools: [{ name: "a" }, { name: "b", mode: "sequential" }],
		turns: [
			{ toolCalls: [{ id: "c1", name: "a", args: { v: 1 } }, { id: "c2", name: "b", args: { v: 2 } }], stopReason: "toolUse" },
			{ text: "done", stopReason: "stop" },
		],
	},
	{
		name: "terminate-batch",
		prompt: "stop",
		tools: [{ name: "stop", behavior: "stop" }],
		turns: [{ toolCalls: [{ id: "c1", name: "stop", args: { v: 1 } }, { id: "c2", name: "stop", args: { v: 2 } }], stopReason: "toolUse" }],
	},
	{
		name: "length-stop",
		prompt: "long",
		tools: [{ name: "a" }],
		turns: [{ toolCalls: [{ id: "c1", name: "a", args: { v: 1 } }], stopReason: "length" }],
	},
	{
		name: "unknown-tool-and-throw",
		prompt: "mixed",
		tools: [{ name: "boom", behavior: "boom" }],
		turns: [
			{ toolCalls: [{ id: "c1", name: "missing", args: {} }, { id: "c2", name: "boom", args: {} }], stopReason: "toolUse" },
			{ text: "after", stopReason: "stop" },
		],
	},
	{
		name: "blocked-call",
		prompt: "block",
		tools: [{ name: "a" }],
		block: "a",
		turns: [
			{ toolCalls: [{ id: "c1", name: "a", args: { v: 1 } }], stopReason: "toolUse" },
			{ text: "after", stopReason: "stop" },
		],
	},
	{
		name: "after-rewrite",
		prompt: "rewrite",
		tools: [{ name: "a" }],
		rewrite: true,
		turns: [
			{ toolCalls: [{ id: "c1", name: "a", args: { v: 1 } }], stopReason: "toolUse" },
			{ text: "after", stopReason: "stop" },
		],
	},
	{
		name: "steering",
		prompt: "first",
		tools: [],
		turns: [
			{ text: "one", stopReason: "stop", steer: [{ role: "user", content: [{ type: "text", text: "typed while waiting" }], timestamp: 2 }] },
			{ text: "two", stopReason: "stop" },
		],
	},
];

const fixtures = [];
for (const scenario of scenarios) {
	const events = [];
	const toolSet = tools(scenario.tools);
	const config = {
		model,
		convertToLlm: (messages) => messages,
		toolExecution: "parallel",
		...(scenario.block
			? {
					beforeToolCall: async ({ toolCall }) =>
						toolCall.name === scenario.block ? { block: true, reason: `blocked ${toolCall.name}` } : undefined,
				}
			: {}),
		...(scenario.rewrite
			? {
					afterToolCall: async () => ({
						content: [{ type: "text", text: "rewritten" }],
						details: { rewritten: true },
						isError: true,
					}),
				}
			: {}),
	};
	const context = {
		messages: [],
		tools: Object.values(toolSet),
	};
	const prompts = [{ role: "user", content: [{ type: "text", text: scenario.prompt }], timestamp: 0 }];
	const steeringQueue = [];
	const pending = [...scenario.turns];
	config.getSteeringMessages = async () => {
		const queued = steeringQueue.slice();
		steeringQueue.length = 0;
		return queued;
	};
	const streamFn = scriptedStream(scenario);
	const wrappedStream = () => {
		const stream = streamFn();
		const turn = pending.shift();
		if (turn?.steer) steeringQueue.push(...turn.steer);
		return stream;
	};
	const messages = await runAgentLoop(prompts, context, config, (event) => {
		events.push(projectEvent(event));
	}, undefined, wrappedStream);
	fixtures.push({ name: scenario.name, events, messages: messages.map(projectMessage) });
}
console.log(JSON.stringify(fixtures, null, 1));
