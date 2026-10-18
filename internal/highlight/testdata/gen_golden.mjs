// Regenerates the highlight.js goldens from the checked-in fixtures.
//
// Usage: node internal/highlight/testdata/gen_golden.mjs [fixture ...]
//
// With no arguments every entry in golden_manifest.json is regenerated; with
// fixture names only those entries are. The reference is the pinned pi
// checkout's highlight.js (10.7.3), loaded through the same entry pi's
// loadAllHighlightLanguages() imports (lib/index.js), so the golden is the
// reference's full scope output.
import { createRequire } from "module";
import fs from "fs";
import path from "path";
import { fileURLToPath } from "url";

const require = createRequire(import.meta.url);
const here = path.dirname(fileURLToPath(import.meta.url));
const repoRoot = path.resolve(here, "..", "..", "..");
const hljs = require(path.join(repoRoot, "pi", "node_modules", "highlight.js", "lib", "index.js"));

const manifest = JSON.parse(fs.readFileSync(path.join(here, "golden_manifest.json"), "utf8"));
const only = new Set(process.argv.slice(2));

let count = 0;
for (const entry of manifest) {
	if (only.size > 0 && !only.has(entry.file)) continue;
	const source = fs.readFileSync(path.join(here, entry.file), "utf8");
	const html = hljs.highlight(source, { language: entry.language }).value;
	fs.writeFileSync(path.join(here, entry.file + ".html"), html);
	count++;
}
console.error(`regenerated ${count} golden(s)`);
