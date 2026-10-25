// Reference: pi v1.4.0 (4a080252), packages/ai/src/utils/overflow.ts.
// Regenerate from repository root:
// node ai/testdata/overflow/generate.mjs ./pi > ai/testdata/overflow/golden.json
// Executes the pinned isContextOverflow for every case.
import { resolve, join } from "node:path";
import { pathToFileURL } from "node:url";

const root = resolve(process.argv[2] ?? "./pi");
const { isContextOverflow } = await import(pathToFileURL(join(root, "packages/ai/src/utils/overflow.ts")).href);

const message = (provider, stopReason, errorMessage, usage = {}, contextWindow = undefined) => ({
	provider,
	stopReason,
	errorMessage,
	usage: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0, totalTokens: 0, cost: {}, ...usage },
	contextWindow,
});

const texts = [
	"prompt is too long: 213462 tokens > 200000 maximum",
	"PROMPT IS TOO LONG",
	"Prompt Too Long",
	"ＰＲＯＭＰＴ TOO LONG",
	"prompt\u00a0is too long",
	"prompt too long",
	"prompt exceeds max length",
	"413 {\"error\":{\"type\":\"request_too_large\",\"message\":\"Request exceeds the maximum size\"}}",
	"request_too_large",
	"input is too long for requested model",
	"Your input exceeds the context window of this model",
	"Requested token count exceeds the model's maximum context length of 131072 tokens",
	"exceeds model's maximum context length (262144)",
	"The input token count (1196265) exceeds the maximum number of tokens allowed (1048575)",
	"This model's maximum prompt length is 131072 but the request contains 537812 tokens",
	"Please reduce the length of the messages or completion",
	"This endpoint's maximum context length is 200000 tokens. However, you requested about 300000 tokens",
	"Input length 300000 exceeds the maximum allowed input length of 200000 tokens.",
	"The input (265330 tokens) is longer than the model's context length (262144 tokens).",
	"the request exceeds the available context size, try increasing it",
	"tokens to keep from the initial prompt is greater than the context length",
	"prompt token count of 300000 exceeds the limit of 200000",
	"github-copilot API error (413): {\"message\":\"failed to parse request\",\"code\":\"\"}",
	"invalid params, context window exceeds limit",
	"Your request exceeded model token limit: 300000 (requested: 200000)",
	"Prompt has 300000 tokens, but the configured context size is 200000 tokens",
	"400 status code (no body)",
	"413 status code (no body)",
	"400 (no body)",
	"Prompt contains 300000 tokens too large for model with 200000 maximum context length",
	"model_context_window_exceeded",
	"prompt too long; exceeded max context length",
	"prompt too long; exceeded context length by 5 tokens",
	"Range of input length should be [1, 200000]",
	"context_length_exceeded",
	"context length exceeded",
	"Too many tokens",
	"token limit exceeded",
	"413",
	"413 ",
	"error 413 occurred",
	"note: status 413 in body",
	"gemini API error (413): request too large",
	"Throttling error: Too many tokens, please wait before trying again",
	"Service unavailable: prompt too long",
	"Rate limit reached: token limit exceeded",
	"Too Many Requests: prompt exceeds max length",
	"Connection reset by peer",
	"",
	"   ",
];

const cases = [];
for (const [index, errorMessage] of texts.entries()) {
	cases.push({ name: `anthropic/${index}`, ...message("anthropic", "error", errorMessage) });
	cases.push({ name: `cerebras/${index}`, ...message("cerebras", "error", errorMessage) });
}
cases.push(
	{ name: "stop/over-window", ...message("zai", "stop", undefined, { input: 100001, cacheRead: 0 }, 100000) },
	{ name: "stop/at-window", ...message("zai", "stop", undefined, { input: 100000, cacheRead: 0 }, 100000) },
	{ name: "stop/cache-read-counts", ...message("zai", "stop", undefined, { input: 60000, cacheRead: 40001 }, 100000) },
	{ name: "stop/no-window", ...message("zai", "stop", undefined, { input: 100001, cacheRead: 0 }, undefined) },
	{ name: "length/zero-output-full", ...message("mimo", "length", undefined, { input: 99000, cacheRead: 0, output: 0 }, 100000) },
	{ name: "length/zero-output-under", ...message("mimo", "length", undefined, { input: 98000, cacheRead: 0, output: 0 }, 100000) },
	{ name: "length/some-output", ...message("mimo", "length", undefined, { input: 100000, cacheRead: 0, output: 5 }, 100000) },
	{ name: "length/no-window", ...message("mimo", "length", undefined, { input: 100000, cacheRead: 0, output: 0 }, undefined) },
	{ name: "tool-use/ignored", ...message("anthropic", "toolUse", "prompt is too long") },
	{ name: "stop-with-error-text", ...message("anthropic", "stop", "prompt is too long") },
);

const output = cases.map((test) => ({
	name: test.name,
	provider: test.provider,
	stopReason: test.stopReason,
	errorMessage: test.errorMessage,
	usage: test.usage,
	contextWindow: test.contextWindow,
	expected: isContextOverflow(
		{ role: "assistant", content: [], api: "x", model: "m", timestamp: 0, ...test },
		test.contextWindow,
	),
}));
console.log(JSON.stringify(output, null, 2));
