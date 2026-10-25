// pi v1.4.0 packages/mcp/src/oauth/flow.ts, through the public token exchange.
// node --experimental-transform-types mcp/oauth/testdata/token-auth/generate.mjs ./pi > mcp/oauth/testdata/token-auth/fixtures.json
import { resolve } from "node:path";
import { pathToFileURL } from "node:url";
const root = resolve(process.argv[2] ?? "./pi");
const { exchangeAuthorizationCode } = await import(pathToFileURL(`${root}/packages/mcp/src/oauth/flow.ts`).href);
const cases = [
	{ name: "public client", client: { client_id: "fixture-client" } },
	{ name: "empty secret", client: { client_id: "fixture-client", client_secret: "" } },
	{ name: "nonempty secret", client: { client_id: "fixture-client", client_secret: "fixture-secret" } },
	{ name: "empty secret with basic hint", client: { client_id: "fixture-client", client_secret: "", token_endpoint_auth_method: "client_secret_basic" } },
	{ name: "empty secret with post hint", client: { client_id: "fixture-client", client_secret: "", token_endpoint_auth_method: "client_secret_post" } },
	{ name: "empty secret with supported basic", client: { client_id: "fixture-client", client_secret: "" }, supported: ["client_secret_basic"] },
	{ name: "nonempty secret with post hint", client: { client_id: "fixture-client", client_secret: "fixture-secret", token_endpoint_auth_method: "client_secret_post" } },
	{ name: "empty resource", client: { client_id: "fixture-client" }, resource: "" },
	{ name: "nonempty resource", client: { client_id: "fixture-client" }, resource: "https://resource.example/mcp" },
];
const output = [];
for (const item of cases) {
	let request;
	let error;
	try {
		await exchangeAuthorizationCode("https://auth.example", {
			clientInformation: item.client, resource: item.resource,
			code: "fixture-code", codeVerifier: "fixture-verifier", redirectUrl: "http://127.0.0.1/callback",
			metadata: item.supported ? { token_endpoint: "https://auth.example/token", token_endpoint_auth_methods_supported: item.supported } : undefined,
			fetch: async (url, init) => {
				request = { authorization: init.headers.get("Authorization"), body: String(init.body), params: Object.fromEntries(init.body) };
				return new Response(JSON.stringify({ access_token: "fixture-token", token_type: "Bearer" }), { status: 200 });
			},
		});
	} catch (caught) { error = caught.message; }
	output.push({ ...item, request, error });
}
console.log(JSON.stringify(output, null, 2));
