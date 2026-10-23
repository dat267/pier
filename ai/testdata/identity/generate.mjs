// Regenerate from the repository root:
// node --experimental-strip-types ai/testdata/identity/generate.mjs ./pi > ai/testdata/identity/golden.json
import { pathToFileURL } from "node:url";
import { resolve } from "node:path";
const { mergeClientIdentity, PI_ATTRIBUTION } = await import(
	pathToFileURL(resolve(process.argv[2], "packages/ai/src/utils/identity.ts"))
);
const roundTrips = [
	{},
	{ userAgent: "short" },
	{ version: "", attribution: null },
	{ version: "9.9.9", userAgent: "detailed" },
	{ userAgent: { custom: "" } },
	{ userAgent: { custom: "short" } },
	{ userAgent: { extension: true, custom: "acme" } },
	{ userAgent: { custom: "<acme>&\u2028\u2029" } },
	{ attribution: { name: "acme", url: "https://acme.example" } },
	{ attribution: { categories: [], url: "https://acme.example", name: "acme" } },
	{ attribution: PI_ATTRIBUTION, version: "1.0.0" },
	{ extension: { 2: "two", 1: "one", html: "<&>" }, version: "9" },
];
const sources = [
	[undefined, undefined],
	[undefined, {}],
	[{ attribution: PI_ATTRIBUTION, userAgent: "short" }, { version: "1.0.0" }],
	[{ attribution: PI_ATTRIBUTION }, { attribution: null }],
	[{ version: "base" }, { version: "" }],
	[{ userAgent: { extension: true, custom: "acme" } }, { version: "2" }],
	[{ userAgent: { custom: "acme" } }, { userAgent: "short" }],
	[{ userAgent: "short" }, { userAgent: { custom: "short" } }],
	[{ version: "base" }, { userAgent: "short" }, { attribution: null }],
	[{ extension: true, version: "9", attribution: PI_ATTRIBUTION, userAgent: "short" },
	 { attribution: null, userAgent: { custom: "" }, version: "" }],
];
console.log(JSON.stringify({
	roundTrips: roundTrips.map(value => JSON.stringify(value)),
	merges: sources.map(values => ({
		sources: values.map(value => value === undefined ? null : value),
		want: JSON.stringify(mergeClientIdentity(...values)) ?? null,
	})),
}, null, 2));
