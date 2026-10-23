// Regenerate from the repository root:
// node --experimental-strip-types ai/testdata/bedrock-identity/generate.mjs ./pi > ai/testdata/bedrock-identity/golden.json
import { readFileSync } from "node:fs";
import { createRequire, stripTypeScriptTypes } from "node:module";
import { resolve } from "node:path";
import { pathToFileURL } from "node:url";
const root = resolve(process.argv[2]);
const require = createRequire(resolve(root, "packages/ai/package.json"));
const { getUserAgentPlugin, resolveUserAgentConfig } = require("@aws-sdk/core/client");
const { HttpRequest } = require("@smithy/core/protocols");
const { getClientVersion } = await import(pathToFileURL(resolve(root, "packages/ai/src/utils/identity.ts")));
const source = readFileSync(resolve(root, "packages/ai/src/api/bedrock-converse-stream.ts"), "utf8");
const start = source.indexOf("function bedrockUserAgent(");
if (start < 0) throw new Error("bedrockUserAgent not found");
const end = source.indexOf("\n}\n", start) + 2;
const bedrockUserAgent = new Function("getClientVersion",
	`return (${stripTypeScriptTypes(source.slice(start, end))});`)(getClientVersion);
const identities = [
	null,
	{ version: "9.9.9" },
	{ version: "" },
	{ userAgent: "short", version: "ignored" },
	{ userAgent: { custom: "acme/1.0" } },
	{ userAgent: { custom: "" } },
	{ userAgent: { custom: "short" } },
	{ userAgent: { custom: "api/MyAPI/One" } },
	{ userAgent: { custom: "/" } },
	{ userAgent: { custom: "//" } },
	{ userAgent: { custom: "acme a:b#c/é😀" } },
	{ version: "a/b#c é😀" },
];
const fixtures = [];
for (const identity of identities) {
	const custom = bedrockUserAgent(identity ?? undefined);
	const options = resolveUserAgentConfig({
		runtime: "node", customUserAgent: custom,
		defaultUserAgentProvider: async () => [["aws-sdk-js", "fixture"]],
		userAgentAppId: async () => undefined,
	});
	let middleware;
	getUserAgentPlugin(options).applyToStack({ add: value => { middleware = value; } });
	const request = new HttpRequest({ protocol: "https:", hostname: "example.test", method: "POST", headers: {} });
	await middleware(async () => ({}), {})({ request });
	const uaPrefix = "aws-sdk-js/fixture m/ ";
	const amzPrefix = "aws-sdk-js/fixture ";
	if (!request.headers["user-agent"].startsWith(uaPrefix) || !request.headers["x-amz-user-agent"].startsWith(amzPrefix)) {
		throw new Error(`unexpected SDK user agent: ${JSON.stringify(request.headers)}`);
	}
	fixtures.push({ identity, custom, want: request.headers["user-agent"].slice(uaPrefix.length),
		amzWant: request.headers["x-amz-user-agent"].slice(amzPrefix.length) });
}
console.log(JSON.stringify(fixtures, null, 2));
