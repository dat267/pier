// Prints the CSS keyword lists extracted from the reference's css language as
// JSON, so the port's copies are checked against the pin rather than trusted.
//
//	node internal/highlight/testdata/css-lists/generate.mjs ./pi
import { readFileSync } from "node:fs";
import { join, resolve } from "node:path";

const root = resolve(process.argv[2] ?? "./pi");
const source = readFileSync(join(root, "node_modules", "highlight.js", "lib", "languages", "css.js"), "utf8");

function entries(name, endMarker) {
	const start = source.indexOf(`const ${name} = [`);
	if (start < 0) throw new Error(`missing ${name}`);
	const end = source.indexOf(endMarker, start);
	if (end < 0) throw new Error(`missing end of ${name}`);
	return [...source.slice(start, end).matchAll(/'([^']*)'/g)].map((match) => match[1]);
}

console.log(
	JSON.stringify({
		tags: entries("TAGS", "\n];"),
		mediaFeatures: entries("MEDIA_FEATURES", "\n];"),
		pseudoClasses: entries("PSEUDO_CLASSES", "\n];"),
		pseudoElements: entries("PSEUDO_ELEMENTS", "\n];"),
		// The attribute list is followed by a comment, not a blank line.
		attributes: entries("ATTRIBUTES", "// reverse makes sure"),
	}),
);
