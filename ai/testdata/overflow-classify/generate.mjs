// Regenerate: node --experimental-strip-types ai/testdata/overflow-classify/generate.mjs ./pi > ai/testdata/overflow-classify/golden.json
//
// Each expectation is pi v1.5.0 (f9a26365) utils/overflow.ts `isContextOverflow`
// for that message. The error strings are the providers the reference's own
// documentation lists, the two silent-overflow signals (usage over the window,
// and a length stop with no output), and case and Unicode lookalike variants of
// a few of them, where the port emulates the reference's case-insensitive
// matching over ASCII letters.
import { resolve } from "node:path";
import { pathToFileURL } from "node:url";

const root = resolve(process.argv[2] ?? "./pi");
const { isContextOverflow } = await import(pathToFileURL(resolve(root, "packages/ai/src/utils/overflow.ts")));

const documented = [
	"prompt is too long: 210000 tokens > 200000 maximum",
	"request_too_large",
	"This model's maximum context length is 128000 tokens. However, your messages resulted in 140000 tokens.",
	"exceeds the context window",
	"exceeds the model's maximum context length of 8192 tokens",
	"exceeds model's maximum context length (128000)",
	"input token count exceeds the maximum number of tokens allowed",
	"maximum prompt length is 4096 but request contains 5000",
	"Please reduce the length of the messages",
	"failed to parse request",
	"Prompt contains 5000 tokens, which is too large for model with 4096 maximum context length",
	"maximum context length is 8192 tokens",
	"Input length 5000 exceeds the maximum allowed input length of 4096 tokens.",
	"The input (5000 tokens) is longer than the model's context length (4096 tokens).",
	"exceeds the available context size",
	"greater than the context length",
	"exceeded model token limit: 4096 (requested: 5000)",
	"Prompt has 5000 tokens, but the configured context size is 4096 tokens",
	"Range of input length should be [1, 4096]",
	"Prompt too long",
	// Matches an overflow pattern and a non-overflow pattern at once: the
	// non-overflow side must win.
	"Too many requests: too many tokens",
	"rate limit reached while sending context_length_exceeded",
	"429 Too Many Requests: token limit exceeded",
	"413 rate limit exceeded: too many tokens",
	"Throttling error: context length exceeded",
	"Service unavailable: too many tokens",
	// Not overflow: the non-overflow patterns must win.
	"Rate limit reached",
	"Throttling error: rate exceeded",
	"Service unavailable: try later",
	"401 Unauthorized",
	"invalid api key",
	"Input is too long for requested model.",
];
const providers = ["openai", "anthropic", "cerebras", "google", "zai", "together", "xai"];

const cases = [];
const errorMessage = (provider, text) => ({
	provider, stopReason: "error", errorMessage: text, contextWindow: 0, usage: { input: 0, output: 0, cacheRead: 0 },
});
for (const text of documented) for (const provider of providers) cases.push(errorMessage(provider, text));
for (const text of [
	"prompt is too long: 210000 tokens > 200000 maximum",
	"exceeds the context window",
	"maximum context length is 8192 tokens",
	"Prompt too long",
	"Range of input length should be [1, 4096]",
]) {
	cases.push(errorMessage("openai", text.toUpperCase()));
	cases.push(errorMessage("openai", text.toLowerCase()));
	cases.push(errorMessage("openai", [...text].map((c, i) => (i % 2 ? c.toUpperCase() : c.toLowerCase())).join("")));
	cases.push(errorMessage("openai", `prefix ${text} suffix`));
	cases.push(errorMessage("openai", text.replace(/k/g, "\u212A").replace(/s/g, "\u017F")));
	cases.push(errorMessage("openai", text.replace(/i/g, "\u0130")));
	cases.push(errorMessage("openai", text.replace(/e/g, "\u0435")));
}
cases.push({ provider: "zai", stopReason: "stop", contextWindow: 200000, usage: { input: 210000, output: 5, cacheRead: 0 } });
cases.push({ provider: "zai", stopReason: "stop", contextWindow: 200000, usage: { input: 190000, output: 5, cacheRead: 10000 } });
cases.push({ provider: "xiaomi", stopReason: "length", contextWindow: 200000, usage: { input: 199000, output: 0, cacheRead: 0 } });
cases.push({ provider: "xiaomi", stopReason: "length", contextWindow: 200000, usage: { input: 100000, output: 0, cacheRead: 0 } });
cases.push({ provider: "openai", stopReason: "length", contextWindow: 200000, usage: { input: 10, output: 0, cacheRead: 0 } });

const assistant = (c) => ({
	role: "assistant", content: [], api: "x", provider: c.provider, model: "m", stopReason: c.stopReason,
	usage: { input: c.usage.input, output: c.usage.output, cacheRead: c.usage.cacheRead, cacheWrite: 0, totalTokens: 0,
		cost: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0, total: 0 } },
	...(c.errorMessage === undefined ? {} : { errorMessage: c.errorMessage }), timestamp: 1,
});
console.log(JSON.stringify(cases.map((c) => ({ ...c, want: isContextOverflow(assistant(c), c.contextWindow || undefined) })), null, 1));
