// Reference: pi v1.5.2 (b4afdddf), packages/tui/src/fuzzy.ts (fuzzyMatch,
// fuzzyFilter): subsequence matching with a lower-is-better score, plus the
// swapped alphanumeric retry and the filter's ordering. The port also has an
// ASCII fast path that reports the same scores, so this corpus covers case,
// camelCase and snake_case boundaries, digits, punctuation separators, wide and
// combining graphemes, and queries that miss.
//
// Regenerate from the repository root:
//   node --experimental-strip-types tui/testdata/fuzzy/generate.mjs ./pi > tui/testdata/fuzzy/golden.json
import { resolve } from "node:path";
import { pathToFileURL } from "node:url";

const root = resolve(process.argv[2] ?? "./pi");
const { fuzzyMatch, fuzzyFilter } = await import(pathToFileURL(resolve(root, "packages/tui/src/fuzzy.ts")).href);

const candidates = [
	"", "a", "abc", "a.b.c", "foo.bar", "readTool", "ReadTool", "read_tool", "read-tool", "READ",
	"café", "cafe", "日本語", "😀a", "a😀b", "x123a456", "123abc", "model/gpt-4o", "gpt4o",
	"claude-sonnet-4-5", "slack-mcp__post_message", "mcp__srv__tool_name", "ALLCAPS", "aA", "Aa",
	"aa", "a_a", "a/a", "a:a", "/start", "ends/", "  spaced  ", "tab\there", "dot.dot.dot",
	"under_score_", "-kebab-", "x".repeat(50), "aaaa", "ababab", "é", "É", "ﬁle", "straße", "Ǆ",
	"a b", "a.b", "ab.cd", "x_y_z", "Ünïcode",
];
const queries = [
	"", "a", "ab", "abc", "a_b", "a-b", "foo", "foo.bar", "readTool", "rt", "mcp", "tool",
	"gpt", "4o", "as", "bs", "é", "É", "日", "😀", "aa", "aaa", "zz", "x", "s", "1", "a1", "1a",
	"READ", "read", "/", ".", "_", "-", ":", "  ", "ﬁ", "abcabc", "a/b",
];

const records = [];
for (const query of queries) {
	const matches = candidates.map((text) => {
		const match = fuzzyMatch(query, text);
		return { text, matches: match.matches, score: match.score };
	});
	records.push({ query, matches });
	records.push({ query, candidates, order: fuzzyFilter(candidates.slice(), query, (text) => text) });
}

process.stdout.write(`${JSON.stringify(records, null, "\t")}\n`);
