// Regenerate from the repository root:
// node coding/testdata/mcp-config/generate.mjs ./pi > coding/testdata/mcp-config/golden.json
// Reference: pi v1.4.0 (4a080252), packages/coding-agent/src/core/mcp-servers.ts
// (validateMcpServerConfig) and src/extensions/mcp/config.ts (loadMcpConfig).
// Executes the pinned functions, never an installed bundle.
import { resolve, join } from "node:path";
import { pathToFileURL } from "node:url";
import { mkdtempSync, writeFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";

const root = resolve(process.argv[2] ?? "./pi");
const { validateMcpServerConfig } = await import(
	pathToFileURL(join(root, "packages/coding-agent/src/core/mcp-servers.ts")).href,
);
const { loadMcpConfig } = await import(
	pathToFileURL(join(root, "packages/coding-agent/src/extensions/mcp/config.ts")).href,
);

const validateCases = [
  ["url-type-number", "srv", { url: "https://example.com/mcp", type: 5 }],
  ["url-type-null", "srv", { url: "https://example.com/mcp", type: null }],
  ["url-type-bool", "srv", { url: "https://example.com/mcp", type: true }],
  ["url-type-array", "srv", { url: "https://example.com/mcp", type: [] }],
  ["command-type-number", "srv", { command: "npx", type: 5 }],
  ["command-type-null", "srv", { command: "npx", type: null }],
  ["url-type-http", "srv", { url: "https://example.com/mcp", type: "http" }],
  ["url-type-streamable", "srv", { url: "https://example.com/mcp", type: "streamable-http" }],
  ["url-type-unknown-string", "srv", { url: "https://example.com/mcp", type: "foo" }],
  ["command-type-stdio", "srv", { command: "npx", type: "stdio" }],
  ["url-and-command", "srv", { url: "https://example.com/mcp", command: "npx" }],
  ["command-and-url-type-stdio", "srv", { command: "npx", url: "https://example.com/mcp", type: "stdio" }],
  ["invalid-name-quote", 'a"b', { command: "x" }],
  ["invalid-name-backslash", "a\\b", { command: "x" }],
  ["invalid-name-space", "a b", { command: "x" }],
  ["invalid-name-unicode", "serveur-général", { command: "x" }],
  ["timeout-string", "srv", { command: "x", timeout: "5" }],
  ["timeout-zero", "srv", { command: "x", timeout: 0 }],
  ["timeout-negative", "srv", { command: "x", timeout: -1 }],
  ["timeout-fraction", "srv", { command: "x", timeout: 1.5 }],
  ["exposure-alias", "srv", { command: "x", exposure: "tool" }],
  ["exposure-invalid", "srv", { command: "x", exposure: "nope" }],
  ["tool-exposure-multi-invalid", "srv", { command: "x", toolExposure: { z: "nope", a: "bad" } }],
  ["exposure-alias-real", "srv", { command: "x", exposure: "codemode-deferred" }],
  ["tool-exposure-pattern-alias", "srv", { command: "x", toolExposure: { "read_*": "codemode-deferred" } }],
  ["headers-numeric", "srv", { url: "https://example.com/mcp", headers: { A: 1 } }],
  ["headers-null", "srv", { url: "https://example.com/mcp", headers: null }],
  ["args-mixed", "srv", { command: "x", args: ["a", 1] }],
  ["args-null", "srv", { command: "x", args: null }],
  ["env-null", "srv", { command: "x", env: null }],
  ["cwd-number", "srv", { command: "x", cwd: 1 }],
  ["cwd-null", "srv", { command: "x", cwd: null }],
  ["url-number", "srv", { url: 5 }],
  ["url-empty", "srv", { url: "" }],
  ["url-ftp", "srv", { url: "ftp://example.com/x" }],
  ["url-uppercase-scheme", "srv", { url: "HTTPS://EXAMPLE.COM/MCP" }],
  ["url-whitespace", "srv", { url: " https://example.com/mcp " }],
  ["url-relative", "srv", { url: "/mcp" }],
  ["url-with-fragment", "srv", { url: "https://example.com/mcp#x" }],
  ["auth-empty-provider", "srv", { url: "https://example.com/mcp", auth: { provider: "" } }],
  ["auth-http-non-loopback", "srv", { url: "http://example.com/mcp", auth: { provider: "p" } }],
  ["auth-http-localhost", "srv", { url: "http://localhost:8080/mcp", auth: { provider: "p" } }],
  ["auth-http-ipv6", "srv", { url: "http://[::1]:8080/mcp", auth: { provider: "p" } }],
  ["oauth-port-zero", "srv", { url: "https://example.com/mcp", oauth: { callbackPort: 0 } }],
  ["oauth-port-max", "srv", { url: "https://example.com/mcp", oauth: { callbackPort: 65535 } }],
  ["oauth-port-over", "srv", { url: "https://example.com/mcp", oauth: { callbackPort: 65536 } }],
  ["oauth-port-fraction", "srv", { url: "https://example.com/mcp", oauth: { callbackPort: 1.5 } }],
  ["oauth-port-string", "srv", { url: "https://example.com/mcp", oauth: { callbackPort: "80" } }],
  ["oauth-callback-url", "srv", { url: "https://example.com/mcp", oauth: { callbackUrl: "http://127.0.0.1:33418/callback" } }],
  ["oauth-callback-nonloopback", "srv", { url: "https://example.com/mcp", oauth: { callbackUrl: "http://example.com/callback" } }],
  ["oauth-callback-query", "srv", { url: "https://example.com/mcp", oauth: { callbackUrl: "http://localhost/cb?x=1" } }],
  ["oauth-callback-port-mismatch", "srv", { url: "https://example.com/mcp", oauth: { callbackUrl: "http://localhost:8080/cb", callbackPort: 8081 } }],
  ["oauth-client-name-blank", "srv", { url: "https://example.com/mcp", oauth: { clientName: "  " } }],
  // `oauth.clientRegistration` (CIMD) is deliberately unported: CHANGES.md
  // records the field and the /mcp sign-in UI as out of scope, so its three
  // validation rules are not pinned here.
  ["oauth-metadata-http-remote", "srv", { url: "https://example.com/mcp", oauth: { authServerMetadataUrl: "http://example.com/meta" } }],
  ["oauth-metadata-https", "srv", { url: "https://example.com/mcp", oauth: { authServerMetadataUrl: "https://example.com/meta" } }],
  ["oauth-metadata-invalid", "srv", { url: "https://example.com/mcp", oauth: { authServerMetadataUrl: "not a url" } }],
  ["url-leading-space", "srv", { url: " https://example.com/mcp" }],
  ["url-trailing-newline", "srv", { url: "https://example.com/mcp\n" }],
  ["url-inner-space-path", "srv", { url: "https://example.com/a b" }],
  ["url-inner-space-host", "srv", { url: "https://exa mple.com/mcp" }],
  ["url-invalid-port", "srv", { url: "https://example.com:99999/mcp" }],
  ["url-port-zero", "srv", { url: "https://example.com:0/mcp" }],
  ["url-tab", "srv", { url: "\thttps://example.com/mcp" }],
  ["url-only-scheme", "srv", { url: "https:" }],
  ["url-file", "srv", { url: "file:///tmp/mcp" }],
  ["auth-url-whitespace", "srv", { url: " https://example.com/mcp", auth: { provider: "p" } }],
  ["oauth-metadata-whitespace", "srv", { url: "https://example.com/mcp", oauth: { authServerMetadataUrl: " https://example.com/meta" } }],
  ["oauth-callback-whitespace", "srv", { url: "https://example.com/mcp", oauth: { callbackUrl: " http://localhost/cb" } }],
  ["non-object", "srv", "just a string"],
  ["non-object-array", "srv", [1, 2]],
];

const validate = validateCases.map(([name, serverName, raw]) => {
	const result = validateMcpServerConfig(serverName, raw);
	const toolExposureOrder =
		raw && typeof raw === "object" && !Array.isArray(raw) && raw.toolExposure &&
		typeof raw.toolExposure === "object" && !Array.isArray(raw.toolExposure)
			? Object.keys(raw.toolExposure)
			: null;
	return {
		name,
		serverName,
		raw,
		toolExposureOrder,
		...(typeof result === "string" ? { error: result } : { config: result }),
	};
});

const loaderConfigs = {
  "type-null-url": { mcpServers: { srv: { url: "https://example.com/mcp", type: null } } },
  "type-number-command": { mcpServers: { srv: { command: "npx", type: 5 } } },
  "whitespace-url": { mcpServers: { srv: { url: " https://example.com/mcp\n" } } },
  "invalid-name-quote": { mcpServers: { 'a"b': { command: "npx" } } },
  "valid-control": { mcpServers: { ok: { command: "npx", args: ["-y", "server"] } } },
  "type-bool-url": { mcpServers: { srv: { url: "https://example.com/mcp", type: true } } },
  "invalid-name-backslash": { mcpServers: { "a\\b": { command: "npx" } } },
  "timeout-string": { mcpServers: { srv: { command: "npx", timeout: "5" } } },
  "tool-exposure-order": { mcpServers: { srv: { command: "npx", toolExposure: { z: "nope", a: "bad" } } } },
  "callback-whitespace": {
    mcpServers: { srv: { url: "https://example.com/mcp", oauth: { callbackUrl: " http://localhost/cb" } } },
  },
  "sse-rejected": { mcpServers: { srv: { url: "https://example.com/mcp", type: "sse" } } },
};

const load = Object.entries(loaderConfigs).map(([name, config]) => {
	const agentDir = mkdtempSync(join(tmpdir(), "pier-mcp-"));
	try {
		writeFileSync(join(agentDir, "mcp.json"), JSON.stringify(config, null, 2));
		const loaded = loadMcpConfig({ agentDir, cwd: agentDir, projectTrusted: false });
		return {
			name,
			config,
			servers: loaded.servers.map((server) => server.name),
			errors: loaded.errors.map((error) => error.split(agentDir).join("<path>")),
		};
	} finally {
		rmSync(agentDir, { recursive: true, force: true });
	}
});

console.log(JSON.stringify({ validate, load }, null, 2));
