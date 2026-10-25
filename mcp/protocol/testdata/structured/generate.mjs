// pi v1.4.0 packages/mcp/src/protocol/content.ts toLlmContent.
// node mcp/protocol/testdata/structured/generate.mjs ./pi > mcp/protocol/testdata/structured/fixtures.json
import { resolve } from "node:path";
import { pathToFileURL } from "node:url";
const root = resolve(process.argv[2] ?? "./pi");
const { toLlmContent } = await import(pathToFileURL(`${root}/packages/mcp/src/protocol/content.ts`).href);
const inputs = [
	'{"2":"two","1":"one","tail":true}',
	'{"value":1.0,"zero":-0,"exponent":1e2}',
	'{"value":"\\u003c\\u0026\\u003e\\u2028\\u2029"}',
	'{"duplicate":"first","tail":true,"duplicate":"last"}',
	'[null, false, 1e-7]',
	'null',
];
console.log(JSON.stringify(inputs.map(input => ({ input, text: toLlmContent({ content: [], structuredContent: JSON.parse(input) })[0].text })), null, 2));
