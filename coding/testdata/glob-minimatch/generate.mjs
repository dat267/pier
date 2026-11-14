// Reference: minimatch, the library pi's find tool and model-scope resolution
// use (pi v1.5.2 b4afdddf, core/tools/find.ts `new Minimatch(pattern, options)`).
// coding.MatchGlob is a hand port of the subset pier uses, so this corpus pins
// that subset to the library itself rather than to the port's own expectations.
//
// Regenerate from the repository root:
//   node --experimental-strip-types coding/testdata/glob-minimatch/generate.mjs > coding/testdata/glob-minimatch/golden.json
//
// Only supported syntax is listed (no extglobs, no POSIX classes, see
// coding/glob.go). A pattern the library itself rejects is left out: it matches
// nothing there, while the port reads the reversed range as a plain one.
import { createRequire } from "node:module";
import { resolve } from "node:path";

const require = createRequire(import.meta.url);
const { Minimatch } = require(resolve(process.cwd(), "pi/node_modules/minimatch"));

const patterns = [
	"*", "**", "a", "a*", "*a", "a**", "**/a", "a/**", "a/**/b", "**/*.go", "*.go", "?",
	"a?c", "[abc]", "[!abc]", "[^abc]", "[a-z]", "{a,b}", "{a,{b,c}}", "{1..3}", "a/{b,c}/d",
	"\\*", "a\\*b", "[]]", "[a-]", "*.*", "a.b", "A", "***", "**a", "a**b", "a/?", "?/a",
	"a/*/b", "**/**", "a/**/", "{a,b}*", "{a,}b", "a{b,c}", "[a-c]x", "**/*", "*/**",
	"a/**/**/b", "**/b/**", "**/*.{go,ts}", "*.{go,ts}", "{a,b}/**", "{a..c}", "{a..c}*",
	"{1..3}/x", "{01..03}", "[!a-z]", "[!a-c]x", "[a-c][a-c]", "[[]", "[]-]", "[a-]x",
	"[^]a]", "a[.]b", "a\\.b", "\\{a\\}", "a\\{b,c\\}", "{a,b", "a,b}", "{}", "{,}",
	"a/", "/a/", "a/*", "*/a", "*/*", "*/*/*", "a/b*", "a/*b", "*a*", "?*", "*?",
	"a**/b", "a/**b", "**.go", "*.go*", "*.[ch]", "\\**", "a\\/b", "a\\\\b", "A/**",
	"**/A", "{A,a}/b", "[A-Z]",
];

const values = [
	"a", "A", "ab", "a/b", "a/b/c", "a/b/c/d", "b", "b/c", "a.go", "x.go", "x.ts", "a.ts",
	"a/b.go", "A/b", "a/B", ".a", "a/.b", "a/b/.c", "/a", "a/", "//a", "a//b", "", "x",
	"{a}", "a{b}", "a,b", "{}", "1", "2", "3", "01", "02", "03", "]", "[", "-", "a-b",
	"a]b", "a\\b", "a\\/b", "**", "a**b", "ba", "aa", "a.b", "a*b", "a?b", "1/x", "a/1",
	"x/y/z", "abc", "a-c", "a/b/", "//", "aa", "x/a", "a/x/b",
];

const cases = [];
for (const pattern of patterns) {
	let matcher;
	try {
		matcher = new Minimatch(pattern, { nocase: true });
	} catch {
		continue;
	}
	for (const value of values) {
		cases.push({ pattern, value, match: matcher.match(value) });
	}
}
process.stdout.write(JSON.stringify({ cases }, null, 1) + "\n");
