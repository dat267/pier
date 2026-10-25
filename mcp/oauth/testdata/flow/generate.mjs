// Regenerate from the repository root:
// node mcp/oauth/testdata/flow/generate.mjs ./pi > mcp/oauth/testdata/flow/golden.json
// Reference: pi v1.4.0 (4a080252), packages/mcp/src/oauth/flow.ts.
// Executes the pinned request builders with a capturing fetch, never an
// installed bundle.
import { resolve, join } from "node:path";
import { pathToFileURL } from "node:url";

const root = resolve(process.argv[2] ?? "./pi");
const { registerClient, startAuthorization, exchangeAuthorizationCode, refreshAuthorization } = await import(
	pathToFileURL(join(root, "packages/mcp/src/oauth/flow.ts")).href,
);

const maskChallenge = (value) => String(value).replace(/code_challenge=[^&]+/, "code_challenge=<x>");

const registrationCases = {
	"defaults": { clientMetadata: {
		client_name: "pi",
		redirect_uris: ["http://127.0.0.1:33418/callback"],
		grant_types: ["authorization_code", "refresh_token"],
		response_types: ["code"],
		token_endpoint_auth_method: "none",
	} },
	"explicit-application-type": {
		clientMetadata: {
			client_name: "pi",
			redirect_uris: ["http://127.0.0.1:33418/callback"],
			grant_types: ["authorization_code", "refresh_token"],
			response_types: ["code"],
			token_endpoint_auth_method: "none",
			application_type: "web",
		},
	},
	"scope": {
		clientMetadata: {
			client_name: "pi",
			redirect_uris: ["http://127.0.0.1:33418/callback"],
			grant_types: ["authorization_code", "refresh_token"],
			response_types: ["code"],
			token_endpoint_auth_method: "none",
			client_uri: "https://example.com",
		},
		scope: "read write",
	},
	"empty-scope": {
		clientMetadata: {
			client_name: "pi",
			redirect_uris: ["http://127.0.0.1:33418/callback"],
			grant_types: ["authorization_code", "refresh_token"],
			response_types: ["code"],
			token_endpoint_auth_method: "none",
		},
		scope: "",
	},
	"web-redirect": {
		clientMetadata: {
			client_name: "pi",
			redirect_uris: ["https://example.com/callback"],
			grant_types: ["authorization_code", "refresh_token"],
			response_types: ["code"],
			token_endpoint_auth_method: "client_secret_post",
		},
	},
};

const authorizationCases = {
	"basic": {
		clientInformation: { client_id: "cid" },
		redirectUrl: "http://127.0.0.1:33418/callback",
	},
	"full": {
		clientInformation: { client_id: "cid" },
		redirectUrl: "http://127.0.0.1:33418/callback",
		scope: "read write offline_access",
		state: "state-value",
		resource: "https://mcp.example.com/mcp",
	},
	"special-characters": {
		clientInformation: { client_id: "cid" },
		redirectUrl: "http://127.0.0.1:33418/cb~x*y$z",
		scope: "read~write*scope",
		state: "s+t",
		resource: "https://mcp.example.com/mcp~x",
	},
	"endpoint-query": {
		clientInformation: { client_id: "cid" },
		redirectUrl: "http://127.0.0.1:33418/callback",
		scope: "read",
		state: "st",
		metadata: {
			authorization_endpoint: "https://auth.example.com/authorize?audience=x&scope=old&audience=y",
			token_endpoint: "https://auth.example.com/token",
			response_types_supported: ["code"],
			code_challenge_methods_supported: ["S256"],
		},
	},
};

const tokenCases = {
	"exchange-basic": {
		kind: "exchange",
		clientInformation: { client_id: "cid", client_secret: "sec" },
		code: "the-code",
		codeVerifier: "verifier",
		redirectUrl: "http://127.0.0.1:33418/callback",
		resource: "https://mcp.example.com/mcp",
	},
	"exchange-none": {
		kind: "exchange",
		clientInformation: { client_id: "cid" },
		code: "the code",
		codeVerifier: "verifier+value",
		redirectUrl: "http://127.0.0.1:33418/callback",
		metadata: {
			token_endpoint: "https://auth.example.com/token",
			response_types_supported: ["code"],
			token_endpoint_auth_methods_supported: ["none"],
		},
	},
	"refresh-basic": {
		kind: "refresh",
		clientInformation: { client_id: "cid", client_secret: "sec" },
		refreshToken: "rt",
		metadata: {
			token_endpoint: "https://auth.example.com/token",
			response_types_supported: ["code"],
			token_endpoint_auth_methods_supported: ["client_secret_basic"],
		},
	},
	"refresh-post": {
		kind: "refresh",
		clientInformation: { client_id: "cid", client_secret: "sec" },
		refreshToken: "rt~1",
		metadata: {
			token_endpoint: "https://auth.example.com/token",
			response_types_supported: ["code"],
			token_endpoint_auth_methods_supported: ["client_secret_post"],
		},
		resource: "https://mcp.example.com/mcp",
	},
};

const tokenResponse = JSON.stringify({ access_token: "at", token_type: "Bearer", expires_in: 3600 });
const registrationResponse = JSON.stringify({ client_id: "cid", client_secret: "sec", redirect_uris: ["http://127.0.0.1:33418/callback"] });
const captureFetch = (captured, responseBody = tokenResponse) => async (url, init) => {
	captured.push({
		url: String(url),
		method: init.method,
		contentType: String(new Headers(init.headers).get("content-type")),
		authorization: new Headers(init.headers).get("authorization") ?? "",
		body: String(init.body),
	});
	return new Response(responseBody, { status: 200, headers: { "content-type": "application/json" } });
};

const registration = {};
for (const [name, test] of Object.entries(registrationCases)) {
	const captured = [];
	await registerClient("https://auth.example.com", {
		clientMetadata: test.clientMetadata,
		...(test.scope === undefined ? {} : { scope: test.scope }),
		fetch: captureFetch(captured, registrationResponse),
	});
	registration[name] = { input: test, request: captured[0] };
}

const authorization = {};
for (const [name, test] of Object.entries(authorizationCases)) {
	const draft = await startAuthorization("https://auth.example.com", {
		clientInformation: test.clientInformation,
		redirectUrl: test.redirectUrl,
		...(test.scope === undefined ? {} : { scope: test.scope }),
		...(test.state === undefined ? {} : { state: test.state }),
		...(test.resource === undefined ? {} : { resource: test.resource }),
		...(test.metadata === undefined ? {} : { metadata: test.metadata }),
	});
	authorization[name] = { input: test, authorizationUrl: maskChallenge(draft.authorizationUrl), verifierLength: draft.codeVerifier.length };
}

const tokens = {};
for (const [name, test] of Object.entries(tokenCases)) {
	const captured = [];
	const options = {
		clientInformation: test.clientInformation,
		...(test.metadata === undefined ? {} : { metadata: test.metadata }),
		...(test.resource === undefined ? {} : { resource: test.resource }),
		fetch: captureFetch(captured),
	};
	if (test.kind === "exchange") {
		await exchangeAuthorizationCode("https://auth.example.com", {
			...options, code: test.code, codeVerifier: test.codeVerifier, redirectUrl: test.redirectUrl,
		});
	} else {
		await refreshAuthorization("https://auth.example.com", { ...options, refreshToken: test.refreshToken });
	}
	tokens[name] = { input: test, request: captured[0] };
}

console.log(JSON.stringify({ registration, authorization, tokens }, null, 2));
