// Regenerate from the repository root:
// node mcp/oauth/testdata/parse/generate.mjs ./pi > mcp/oauth/testdata/parse/golden.json
// Reference: pi v1.4.0 (4a080252), packages/mcp/src/oauth/{types,discovery,flow}.ts.
// Executes the pinned parse functions and URL helpers, never an installed bundle.
import { resolve, join } from "node:path";
import { pathToFileURL } from "node:url";

const root = resolve(process.argv[2] ?? "./pi");
const types = await import(pathToFileURL(join(root, "packages/mcp/src/oauth/types.ts")).href);
const discovery = await import(pathToFileURL(join(root, "packages/mcp/src/oauth/discovery.ts")).href);
const flow = await import(pathToFileURL(join(root, "packages/mcp/src/oauth/flow.ts")).href);

const tokenCases = {
	"minimal": { access_token: "at", token_type: "Bearer" },
	"full": { access_token: "at", token_type: "Bearer", expires_in: 3600, scope: "read write", refresh_token: "rt", id_token: "id" },
	"expires-string": { access_token: "at", token_type: "Bearer", expires_in: "3600" },
	"expires-fraction": { access_token: "at", token_type: "Bearer", expires_in: 3599.5 },
	"expires-zero": { access_token: "at", token_type: "Bearer", expires_in: 0 },
	"expires-null": { access_token: "at", token_type: "Bearer", expires_in: null },
	"expires-empty": { access_token: "at", token_type: "Bearer", expires_in: "" },
	"expires-bool": { access_token: "at", token_type: "Bearer", expires_in: true },
	"expires-hex": { access_token: "at", token_type: "Bearer", expires_in: "0x10" },
	"expires-space": { access_token: "at", token_type: "Bearer", expires_in: " 3600 " },
	"expires-exponent": { access_token: "at", token_type: "Bearer", expires_in: "1e3" },
	"expires-nan": { access_token: "at", token_type: "Bearer", expires_in: "abc" },
	"expires-object": { access_token: "at", token_type: "Bearer", expires_in: {} },
	"expires-array-empty": { access_token: "at", token_type: "Bearer", expires_in: [] },
	"expires-array-one": { access_token: "at", token_type: "Bearer", expires_in: [5] },
	"scope-empty": { access_token: "at", token_type: "Bearer", scope: "" },
	"scope-null": { access_token: "at", token_type: "Bearer", scope: null },
	"scope-number": { access_token: "at", token_type: "Bearer", scope: 5 },
	"missing-access": { token_type: "Bearer" },
	"empty-access": { access_token: "", token_type: "Bearer" },
	"missing-type": { access_token: "at" },
	"not-object": "nope",
	"array": [1, 2],
	"null": null,
};
// Members outside the typed model (unknown fields, wrongly typed declared
// members, empty arrays) are D275 and stay out of this corpus.
const clientCases = {
	"minimal": { client_id: "cid" },
	"secret-empty": { client_id: "cid", client_secret: "" },
	"secret-null": { client_id: "cid", client_secret: null },
	"secret": { client_id: "cid", client_secret: "s" },
	"redirects": { client_id: "cid", redirect_uris: ["http://127.0.0.1/cb"] },
	"redirects-null": { client_id: "cid", redirect_uris: null },
	"redirects-empty": { client_id: "cid", redirect_uris: [] },
	"redirects-invalid": { client_id: "cid", redirect_uris: "http://127.0.0.1/cb" },
	"redirects-mixed": { client_id: "cid", redirect_uris: ["a", 1] },
	"issued-at": { client_id: "cid", client_id_issued_at: 1700000000 },
	"issued-at-string": { client_id: "cid", client_id_issued_at: "1700000000" },
	"issued-at-null": { client_id: "cid", client_id_issued_at: null },
	"issued-at-fraction": { client_id: "cid", client_id_issued_at: 1.5 },
	"metadata": {
		client_id: "cid", token_endpoint_auth_method: "none", grant_types: ["authorization_code"],
		response_types: ["code"], client_name: "Pier", application_type: "native",
		client_uri: "https://example.com", logo_uri: "https://example.com/logo.png",
		contacts: ["a@example.com"], tos_uri: "https://example.com/tos", policy_uri: "https://example.com/policy",
		jwks_uri: "https://example.com/jwks", jwks: { keys: [] }, software_id: "sid",
		software_version: "1.0", software_statement: "stmt", scope: "read",
	},
	"missing-id": { client_secret: "s" },
	"empty-id": { client_id: "" },
	"not-object": 5,
};
// Unknown and empty-array members are D275.
const protectedCases = {
	"minimal": { resource: "https://example.com/mcp" },
	"full": { resource: "https://example.com/mcp", authorization_servers: ["https://auth.example.com"], scopes_supported: ["read"] },
	"servers-null": { resource: "https://example.com/mcp", authorization_servers: null },
	"servers-mixed": { resource: "https://example.com/mcp", authorization_servers: ["https://a", 5] },
	"server-javascript": { resource: "https://example.com/mcp", authorization_servers: ["javascript:alert(1)"] },
	"server-relative": { resource: "https://example.com/mcp", authorization_servers: ["/auth"] },
	"scopes-mixed": { resource: "https://example.com/mcp", scopes_supported: ["read", 5] },
	"resource-javascript": { resource: "javascript:alert(1)" },
	"resource-relative": { resource: "/mcp" },
	"resource-empty": { resource: "" },
	"resource-whitespace": { resource: " https://example.com/mcp " },
	"not-object": [],
};
// Unknown members are D275.
const serverCases = {
	"minimal": {
		issuer: "https://auth.example.com", authorization_endpoint: "https://auth.example.com/authorize",
		token_endpoint: "https://auth.example.com/token", response_types_supported: ["code"],
	},
	"full": {
		issuer: "https://auth.example.com", authorization_endpoint: "https://auth.example.com/authorize",
		token_endpoint: "https://auth.example.com/token", response_types_supported: ["code"],
		registration_endpoint: "https://auth.example.com/register", scopes_supported: ["read"],
		grant_types_supported: ["authorization_code"], token_endpoint_auth_methods_supported: ["none"],
		code_challenge_methods_supported: ["S256"], client_id_metadata_document_supported: true,
		authorization_response_iss_parameter_supported: true,
	},
	"missing-response-types": {
		issuer: "https://auth.example.com", authorization_endpoint: "https://auth.example.com/authorize",
		token_endpoint: "https://auth.example.com/token",
	},
	"empty-response-types": {
		issuer: "https://auth.example.com", authorization_endpoint: "https://auth.example.com/authorize",
		token_endpoint: "https://auth.example.com/token", response_types_supported: [],
	},
	"registration-empty": {
		issuer: "https://auth.example.com", authorization_endpoint: "https://auth.example.com/authorize",
		token_endpoint: "https://auth.example.com/token", response_types_supported: ["code"], registration_endpoint: "",
	},
	"flag-string": {
		issuer: "https://auth.example.com", authorization_endpoint: "https://auth.example.com/authorize",
		token_endpoint: "https://auth.example.com/token", response_types_supported: ["code"],
		client_id_metadata_document_supported: "true",
	},
	"issuer-whitespace": {
		issuer: " https://auth.example.com", authorization_endpoint: "https://auth.example.com/authorize",
		token_endpoint: "https://auth.example.com/token", response_types_supported: ["code"],
	},
	"issuer-data": {
		issuer: "data:text/plain,x", authorization_endpoint: "https://auth.example.com/authorize",
		token_endpoint: "https://auth.example.com/token", response_types_supported: ["code"],
	},
	"not-object": null,
};

const sortKeys = (value) => {
	if (Array.isArray(value)) return value.map(sortKeys);
	if (value && typeof value === "object") {
		return Object.fromEntries(Object.keys(value).sort().map((key) => [key, sortKeys(value[key])]));
	}
	return value;
};
const capture = (fn, ...args) => {
	try {
		return { ok: true, value: sortKeys(fn(...args)) };
	} catch (error) {
		return { ok: false, error: error.message };
	}
};
const runAll = (cases, fn) =>
	Object.fromEntries(
		Object.entries(cases).map(([name, input]) => {
			const result = capture(fn, input);
			return [name, { input, ...result }];
		}),
	);

const selectResourceCases = [
	["exact", "https://example.com/mcp", "https://example.com/mcp"],
	["trailing-slash", "https://example.com/mcp/", "https://example.com/mcp"],
	["nested", "https://example.com/mcp/sub", "https://example.com/mcp"],
	["root-resource", "https://example.com/mcp", "https://example.com/"],
	["path-prefix-mismatch", "https://example.com/mcpx", "https://example.com/mcp"],
	["different-host", "https://example.com/mcp", "https://other.example.com/mcp"],
	["different-scheme", "https://example.com/mcp", "http://example.com/mcp"],
	["default-port", "https://example.com:443/mcp", "https://example.com/mcp"],
	["other-port", "https://example.com:8443/mcp", "https://example.com/mcp"],
	["uppercase-host", "https://EXAMPLE.com/mcp", "https://example.com/mcp"],
	["encoded-path", "https://example.com/a%2Fb", "https://example.com/a%2Fb"],
	["unicode-path", "https://example.com/%C3%BC", "https://example.com/%C3%BC"],
	["fragment-server", "https://example.com/mcp#frag", "https://example.com/mcp"],
	["query-resource", "https://example.com/mcp", "https://example.com/mcp?x=1"],
	["ipv6", "https://[::1]:8443/mcp", "https://[::1]:8443/mcp"],
	["no-metadata", "https://example.com/mcp", null],
	["whitespace-resource", "https://example.com/mcp", " https://example.com/mcp "],
	["whitespace-server", " https://example.com/mcp ", "https://example.com/mcp"],
];
const selectResource = Object.fromEntries(
	selectResourceCases.map(([name, server, resource]) => {
		const metadata = resource === null ? undefined : { resource };
		const result = capture(discovery.selectResource, server, metadata);
		return [name, { server, resource, metadata: metadata ?? null, ...result }];
	}),
);

// Default-port and host-case normalization is D276; whitespace is WHATWG
// preprocessing and is pinned here.
const resourceUrlCases = [
	["plain", "https://example.com/mcp"],
	["fragment", "https://example.com/mcp#frag"],
	["query", "https://example.com/mcp?x=1"],
	["trailing-slash", "https://example.com/mcp/"],
	["relative", "/mcp"],
	["whitespace", " https://example.com/mcp "],
];
const resourceUrls = Object.fromEntries(
	resourceUrlCases.map(([name, url]) => {
		const result = capture((value) => String(discovery.resourceUrlFromServerUrl(value)), url);
		return [name, { url, ...result }];
	}),
);

const stepUpCases = [
	["none", undefined, undefined],
	["challenged-only", undefined, "read write"],
	["granted-only", "read", undefined],
	["merge", "read", "write"],
	["overlap", "read write", "write admin"],
	["empty-challenged", "read", ""],
	["empty-granted", "", "read"],
	["whitespace", "  read   write  ", " admin "],
	["duplicates", "read read", "read"],
	["tabs-newlines", "read\twrite\nadmin", "extra"],
];
const stepUp = Object.fromEntries(
	stepUpCases.map(([name, granted, challenged]) => {
		const result = capture(flow.stepUpScope, granted, challenged);
		return [name, { granted: granted ?? null, challenged: challenged ?? null, ...result }];
	}),
);

console.log(JSON.stringify({
	tokens: runAll(tokenCases, types.parseOAuthTokens),
	clients: runAll(clientCases, types.parseClientInformation),
	protected: runAll(protectedCases, types.parseProtectedResourceMetadata),
	servers: runAll(serverCases, types.parseAuthorizationServerMetadata),
	selectResource,
	resourceUrls,
	stepUp,
}, null, 2));
