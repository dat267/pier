// Regenerate: node --experimental-strip-types ai/testdata/retry-classify/generate.mjs ./pi > ai/testdata/retry-classify/golden.json
//
// Each expectation is pi v1.5.2 (b4afdddf) utils/retry.ts
// `isRetryableAssistantError` for that error message. The corpus holds every
// needle its two pattern lists document, the texts that must stay
// non-retryable (subscription, quota and billing exhaustion), the status-code
// boundary cases the reference writes with a lookbehind the port cannot use,
// and a deterministic token soup around the same pieces.
import { resolve } from "node:path";
import { pathToFileURL } from "node:url";

const root = resolve(process.argv[2] ?? "./pi");
const { isRetryableAssistantError } = await import(pathToFileURL(resolve(root, "packages/ai/src/utils/retry.ts")));

const messages = [
	"overloaded", "server_busy", "servers are currently busy", "currently experiencing high demand",
	"model is at capacity", "rate limit", "rate-limit", "rateLimit", "ratelimit", "too many requests",
	"Error 429", "429", "status 429", "HTTP 500", "502 Bad Gateway", "503 Service Unavailable", "504",
	"520", "524", "request 429123", "id=429", "429429", "x429", "429x", "429_", "_429", "429abc",
	"code 5001", "1500", "5000 bytes", "a500b",
	"The service is temporarily unavailable", "service unavailable", "ServiceUnavailable",
	"server error", "server_error", "internal error", "internal_error",
	"Provider returned error", "The upstream provider returned an error",
	"exceeded request buffer limit while retrying upstream", "unable to forward upstream",
	"upstream request failed", "upstream_stream_read_error", "stream was interrupted",
	"stream disconnected before completion: stream closed before response.completed",
	"Claude Code's stream from Anthropic was cut short after the reply had begun",
	"stream from Anthropic was cut short", "cut short",
	"HTTP/2 stream failed", "http/2 stream failed",
	"network error", "connection error", "connection refused", "connection lost",
	"other side closed", "fetch failed", "request to https://x failed, reason:", "getaddrinfo ENOTFOUND",
	// Limits that must stay non-retryable even though they look transient.
	"GoUsageLimitError", "FreeUsageLimitError", "Monthly usage limit reached",
	"available balance", "insufficient_quota", "out of budget", "quota exceeded",
	"billing hard limit reached", "subscription_sharing_usage_limit_exceeded",
	"subscription_sharing_workplace_individual_usage_limit_exceeded",
	'"code"\s*:\s*"1308"', '"code": 1308', '"code":13080', '"code": "1308"',
	"Monthly usage limit reached: rate limit", "available balance: rate limit",
	// Near misses.
	"", "unknown", "invalid input", "401 Unauthorized", "model not found",
];
let seed = 987654321;
const rnd = () => (seed = (seed * 1103515245 + 12345) & 0x7fffffff) / 0x7fffffff;
const pieces = ["429", "500", "503", "1", "9", "a", "Z", "é", " ", "_", "-", "x", "rate limit", "billing", "overloaded"];
for (let n = 0; n < 400; n++) {
	let text = "";
	const len = 1 + Math.floor(rnd() * 5);
	for (let i = 0; i < len; i++) text += pieces[Math.floor(rnd() * pieces.length)];
	messages.push(text);
}

const assistant = (text) => ({
	role: "assistant", content: [], api: "x", provider: "openai", model: "m", stopReason: "error",
	errorMessage: text, timestamp: 1,
	usage: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0, totalTokens: 0,
		cost: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0, total: 0 } },
});
console.log(JSON.stringify(messages.map((text) => ({ errorMessage: text, want: isRetryableAssistantError(assistant(text)) })), null, 1));
