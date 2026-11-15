// Prints the reference's language registration order as JSON. Auto-detection
// iterates Object.keys(languages), i.e. lib/index.js's registerLanguage call
// order, and equal-relevance ties settle by it.
//
//	node --experimental-strip-types internal/highlight/testdata/language-order/generate.mjs ./pi
import { readFileSync } from "node:fs";
import { join, resolve } from "node:path";

const root = resolve(process.argv[2] ?? "./pi");
const source = readFileSync(join(root, "node_modules", "highlight.js", "lib", "index.js"), "utf8");
const names = [...source.matchAll(/registerLanguage\('([^']+)'/g)].map((match) => match[1]);
if (names.length === 0) throw new Error("no registerLanguage calls found");
console.log(JSON.stringify(names));
