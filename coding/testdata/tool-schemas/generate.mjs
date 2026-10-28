// Regenerate: node coding/testdata/tool-schemas/generate.mjs ./pi > coding/testdata/tool-schemas/golden.json
// Evaluate the actual TypeBox schema declarations without importing tool
// renderers or executing tools. This pins insertion order as well as contents.
import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { createRequire } from "node:module";
const root = resolve(process.argv[2] ?? "./pi");
const { Type } = createRequire(resolve(root, "package.json"))("typebox");
const fixtures = [];
for (const name of ["read", "write", "edit", "bash", "grep", "find", "ls"]) {
	const source = readFileSync(resolve(root, `packages/coding-agent/src/core/tools/${name}.ts`), "utf8");
	const schema = source.match(new RegExp(`const ${name}Schema = [\\s\\S]*?;\\n`));
	if (!schema) throw new Error(`Missing ${name} schema`);
	let code = schema[0];
	if (name === "edit") {
		const nested = source.match(/const replaceEditSchema = [\s\S]*?;\n/);
		if (!nested) throw new Error("Missing edit item schema");
		code = nested[0] + code;
	}
	const parameters = Function("Type", code + `;return JSON.stringify(${name}Schema);`)(Type);
	fixtures.push({ name, parameters });
}
console.log(JSON.stringify(fixtures, null, 2));
