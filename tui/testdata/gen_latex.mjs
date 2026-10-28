// Reference: pi v1.4.0 (4a080252), packages/tui/src/latex.ts renderLatex.
// Regenerate from the repository root:
// node --experimental-strip-types tui/testdata/gen_latex.mjs > tui/testdata/latex_golden.txt
// Labels keep the corpus file's order; every entry is rendered inline and in
// display mode, and an unsupported parse is written as null.
import { readFileSync } from "node:fs";
import { renderLatex } from "../../pi/packages/tui/src/latex.ts";

const corpus = JSON.parse(readFileSync(new URL("./latex_corpus.json", import.meta.url), "utf8"));
for (const [label, spec] of Object.entries(corpus)) {
	for (const display of [false, true]) {
		const rendered = renderLatex(spec.text, { display });
		const key = display ? `${label}@display` : label;
		console.log(`latex ${key} => ${rendered === undefined ? "null" : JSON.stringify(rendered)}`);
	}
}
