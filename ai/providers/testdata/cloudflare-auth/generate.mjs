// pi v1.4.0 (4a080252), packages/ai/src/providers/cloudflare-auth.ts.
// Regenerate from the repository root:
// node ai/providers/testdata/cloudflare-auth/generate.mjs ./pi > ai/providers/testdata/cloudflare-auth/fixtures.json
import { resolve } from "node:path";
import { pathToFileURL } from "node:url";

const root = resolve(process.argv[2] ?? "./pi");
const { cloudflareWorkersAIAuth, cloudflareAIGatewayAuth } = await import(
	pathToFileURL(`${root}/packages/ai/src/providers/cloudflare-auth.ts`).href
);
const ambient = {
	CLOUDFLARE_API_KEY: "fixture-key",
	CLOUDFLARE_ACCOUNT_ID: "fixture-account",
	CLOUDFLARE_GATEWAY_ID: "fixture-gateway",
};
const cases = [
	{ name: "ambient workers", kind: "workers-ai" },
	{ name: "ambient gateway", kind: "ai-gateway" },
	{ name: "empty stored key", kind: "workers-ai", credential: { key: "" } },
	{ name: "empty stored account", kind: "workers-ai", credential: { key: "stored-key", env: { CLOUDFLARE_ACCOUNT_ID: "" } } },
	{ name: "empty stored gateway", kind: "ai-gateway", credential: { key: "stored-key", env: { CLOUDFLARE_GATEWAY_ID: "" } } },
	{ name: "missing stored account uses ambient", kind: "workers-ai", credential: { key: "stored-key" } },
	{ name: "empty ambient key", kind: "workers-ai", env: { ...ambient, CLOUDFLARE_API_KEY: "" } },
	{ name: "empty ambient account", kind: "workers-ai", env: { ...ambient, CLOUDFLARE_ACCOUNT_ID: "" } },
	{ name: "empty ambient gateway", kind: "ai-gateway", env: { ...ambient, CLOUDFLARE_GATEWAY_ID: "" } },
];
const output = [];
for (const item of cases) {
	const env = item.env ?? ambient;
	const queries = [];
	const auth = item.kind === "ai-gateway" ? cloudflareAIGatewayAuth() : cloudflareWorkersAIAuth();
	const result = await auth.resolve({
		ctx: { env: async (name) => { queries.push(name); return env[name]; }, fileExists: async () => false },
		credential: item.credential ? { type: "api_key", ...item.credential } : undefined,
		signal: new AbortController().signal,
	});
	output.push({ ...item, env, present: result !== undefined, queries });
}
console.log(JSON.stringify(output, null, 2));
