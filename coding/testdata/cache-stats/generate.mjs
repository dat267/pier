// Regenerate from the repository root:
// node coding/testdata/cache-stats/generate.mjs ./pi > coding/testdata/cache-stats/golden.json
// Reference: pi v1.4.0 (4a080252), packages/coding-agent/src/core/cache-stats.ts.
// Executes the pinned exported functions, never an installed bundle.
import { resolve, join } from "node:path";
import { pathToFileURL } from "node:url";

const root = resolve(process.argv[2] ?? "./pi");
const { detectCacheMiss, computeCacheWaste, collectCacheMisses } = await import(
	pathToFileURL(join(root, "packages/coding-agent/src/core/cache-stats.ts")).href,
);

const usage = (input, cacheRead, cacheWrite, cost = {}) => ({
	input,
	output: 0,
	cacheRead,
	cacheWrite,
	total: input + cacheRead + cacheWrite,
	cost: {
		input: cost.input ?? 0,
		output: 0,
		cacheRead: cost.cacheRead ?? 0,
		cacheWrite: cost.cacheWrite ?? 0,
		total: 0,
	},
});

const assistant = (id, timestamp, sizes, cost) => ({
	type: "message",
	id,
	parentId: null,
	timestamp,
	message: {
		role: "assistant",
		provider: "anthropic",
		model: "claude-opus-4-5",
		timestamp: Number.isNaN(Date.parse(timestamp)) ? 0 : Date.parse(timestamp),
		usage: usage(...sizes, cost),
		content: [],
		stopReason: "stop",
		api: "anthropic-messages",
	},
});

const warm = (id, timestamp, kind, provider, model, sizes) => ({
	type: "usage",
	id,
	parentId: null,
	timestamp,
	kind,
	provider,
	model,
	usage: usage(...sizes),
});

const compaction = (id, timestamp) => ({
	type: "compaction",
	id,
	parentId: null,
	timestamp,
	summary: "summary",
	firstKeptEntryId: "a",
	tokensBefore: 1000,
});

// `entries` is the full branch; the request under test is its last message.
const scenarios = [
	{
		name: "warm-total-miss",
		entries: [
			assistant("a", "2026-01-01T00:00:00.000Z", [10000, 0, 0], { input: 0.03 }),
			warm("w", "2026-01-01T00:01:00.000Z", "cache_warm", "anthropic", "claude-opus-4-5", [0, 12000, 0]),
			assistant("b", "2026-01-01T00:02:00.000Z", [12000, 0, 0], { input: 0.036 }),
		],
	},
	{
		name: "warm-partial-hit",
		entries: [
			warm("w", "2026-01-01T00:01:00.000Z", "cache_warm", "anthropic", "claude-opus-4-5", [0, 20000, 0]),
			assistant("b", "2026-01-01T00:02:00.000Z", [5000, 10000, 0], { input: 0.015, cacheRead: 0.003 }),
		],
	},
	{
		name: "warm-model-change",
		entries: [
			assistant("a", "2026-01-01T00:00:00.000Z", [10000, 0, 0], { input: 0.03 }),
			warm("w", "2026-01-01T00:01:00.000Z", "cache_warm", "openai", "gpt-5", [0, 12000, 0]),
			assistant("b", "2026-01-01T00:02:00.000Z", [12000, 0, 0], { input: 0.036 }),
		],
	},
	{
		name: "warm-zero-prompt-tokens",
		entries: [
			assistant("a", "2026-01-01T00:00:00.000Z", [10000, 0, 0], { input: 0.03 }),
			warm("w", "2026-01-01T00:01:00.000Z", "cache_warm", "anthropic", "claude-opus-4-5", [0, 0, 0]),
			assistant("b", "2026-01-01T00:02:00.000Z", [12000, 0, 0], { input: 0.036 }),
		],
	},
	{
		name: "non-cache-warm-usage",
		entries: [
			assistant("a", "2026-01-01T00:00:00.000Z", [10000, 0, 0], { input: 0.03 }),
			warm("w", "2026-01-01T00:01:00.000Z", "other", "anthropic", "claude-opus-4-5", [0, 12000, 0]),
			assistant("b", "2026-01-01T00:02:00.000Z", [12000, 0, 0], { input: 0.036 }),
		],
	},
	{
		name: "warm-then-compaction-reset",
		entries: [
			assistant("a", "2026-01-01T00:00:00.000Z", [10000, 0, 0], { input: 0.03 }),
			warm("w", "2026-01-01T00:01:00.000Z", "cache_warm", "anthropic", "claude-opus-4-5", [0, 12000, 0]),
			compaction("c", "2026-01-01T00:01:30.000Z"),
			assistant("b", "2026-01-01T00:02:00.000Z", [12000, 0, 0], { input: 0.036 }),
		],
	},
	{
		name: "consecutive-warms",
		entries: [
			warm("w1", "2026-01-01T00:01:00.000Z", "cache_warm", "anthropic", "claude-opus-4-5", [0, 12000, 0]),
			warm("w2", "2026-01-01T00:01:30.000Z", "cache_warm", "anthropic", "claude-opus-4-5", [0, 15000, 0]),
			assistant("b", "2026-01-01T00:02:00.000Z", [15000, 0, 0], { input: 0.045 }),
		],
	},
	{
		name: "warm-invalid-timestamp",
		entries: [
			warm("w", "not-a-date", "cache_warm", "anthropic", "claude-opus-4-5", [0, 12000, 0]),
			assistant("b", "2026-01-01T00:02:00.000Z", [12000, 0, 0], { input: 0.036 }),
		],
	},
	{
		name: "warm-then-full-hit",
		entries: [
			warm("w", "2026-01-01T00:01:00.000Z", "cache_warm", "anthropic", "claude-opus-4-5", [0, 12000, 0]),
			assistant("b", "2026-01-01T00:02:00.000Z", [0, 12000, 0], { cacheRead: 0.0036 }),
		],
	},
];

const models = { getModel: () => undefined };
const cases = scenarios.map((scenario) => {
	const request = scenario.entries[scenario.entries.length - 1].message;
	const priorEntries = scenario.entries.slice(0, -1);
	const miss = detectCacheMiss(priorEntries, request, models) ?? null;
	return {
		...scenario,
		miss,
		totals: computeCacheWaste(scenario.entries, models),
		misses: [...collectCacheMisses(scenario.entries, models).values()],
	};
});

console.log(JSON.stringify({ cases }, null, 2));
