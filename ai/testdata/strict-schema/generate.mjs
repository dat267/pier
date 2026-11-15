// Reference: pi v1.5.2 (b4afdddf) packages/ai/src/api/constrained-sampling.ts
// makeStrictJsonSchema. Run from the repository root:
//   node --experimental-strip-types ai/testdata/strict-schema/generate.mjs ./pi > ai/testdata/strict-schema/golden.json
// Each case records the strict schema JSON exactly as the reference stringifies
// it, or the reference's error message.
import { resolve } from "node:path";
import { pathToFileURL } from "node:url";

const root = resolve(process.argv[2] ?? "./pi");
const { makeStrictJsonSchema } = await import(
	pathToFileURL(resolve(root, "packages/ai/src/api/constrained-sampling.ts")).href
);

const schemas = [
	// Object shape: required properties stay, optional ones become nullable.
	{ type: "object", properties: { a: { type: "string" } }, required: ["a"] },
	{ type: "object", properties: { a: { type: "string" } } },
	{ type: "object", properties: { a: { type: ["string", "null"] } } },
	{ type: "object", properties: { a: { const: null } } },
	{ type: "object", properties: { a: { enum: ["x", null] } } },
	{ type: "object", properties: { a: { anyOf: [{ type: "string" }, { type: null }] } } },
	// Recursion: a nested object, an array of objects, and both together.
	{ type: "object", properties: { nested: { type: "object", properties: { b: { type: "number" } } } } },
	{ type: "object", properties: { list: { type: "array", items: { type: "object", properties: { c: {} } } } } },
	{
		type: "object",
		properties: {
			deep: {
				type: "array",
				items: { type: "object", properties: { d: { type: "object", properties: { e: { type: "string" } } } } },
			},
		},
	},
	// JavaScript key order: integer-like property names come first, ascending.
	{ type: "object", properties: { b: { type: "string" }, "2": { type: "string" }, "1": { type: "string" } } },
	{ type: "object", properties: { x: { type: "object", properties: { "10": {}, "9": {}, a: {} } } } },
	// required is rewritten to the property order, and existing keys keep their slot.
	{ type: "object", required: ["b", "a"], properties: { a: { type: "string" }, b: { type: "string" } } },
	{
		type: "object",
		additionalProperties: false,
		required: ["a"],
		description: "d",
		properties: { a: { type: "string" } },
	},
	{ type: "object", properties: {} },
	{ type: "object", properties: {}, required: [] },
	// anyOf of scalars at the root, and one after other members.
	{ type: "object", properties: { a: { anyOf: [{ type: "string" }, { type: "number", minimum: 0 }] } } },
	{ anyOf: [{ type: "string" }, { type: "null" }], title: "t" },
	// Errors.
	true,
	{ $ref: "#/$defs/x" },
	{ type: "object", properties: { a: { allOf: [{ type: "string" }] } } },
	{ type: "object", properties: { a: { type: "array", items: [{ type: "string" }] } } },
	{ type: "object", anyOf: [] },
	{ type: "object", properties: { a: { anyOf: [{ type: "object", properties: {} }] } } },
	{ properties: { a: { type: "string" } } },
	{ type: "object", additionalProperties: true, properties: { a: { type: "string" } } },
	{ type: "object", additionalProperties: { type: "string" }, properties: { a: { type: "string" } } },
	{ type: "object", properties: { a: { type: "string" } }, required: ["b"] },
	{ type: "object", properties: { a: { type: "string" } }, required: "a" },
	{ type: "object", properties: { a: { type: "string" } }, required: [1] },
	{ type: "object", properties: [] },
	{ type: "object", properties: null },
	{ type: "array", items: { type: "string" } },
	{ type: ["object"], properties: { a: { type: "string" } } },
];

const cases = schemas.map((schema) => {
	try {
		return { schema, strictJson: JSON.stringify(makeStrictJsonSchema(schema)) };
	} catch (error) {
		return { schema, error: String(error?.message ?? error) };
	}
});
process.stdout.write(`${JSON.stringify(cases, null, "\t")}\n`);
