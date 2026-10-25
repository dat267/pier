// Reference: pi v1.4.0 (4a080252), core/{trust-manager,project-trust}.ts
// and extensions/mcp/config.ts. This only loads configs, never starts servers.
// Regenerate from repository root:
// node coding/testdata/project-trust/generate.mjs ./pi > coding/testdata/project-trust/golden.json
import { mkdtempSync, mkdirSync, writeFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { pathToFileURL } from "node:url";

const root = resolve(process.argv[2] ?? "./pi");
const { hasTrustRequiringProjectResources, ProjectTrustStore } = await import(
	pathToFileURL(join(root, "packages/coding-agent/src/core/trust-manager.ts")).href,
);
const { resolveProjectTrusted } = await import(
	pathToFileURL(join(root, "packages/coding-agent/src/core/project-trust.ts")).href,
);
const { loadMcpConfig } = await import(
	pathToFileURL(join(root, "packages/coding-agent/src/extensions/mcp/config.ts")).href,
);
const cases = [
	{ name: "empty", resources: [], policy: "never" },
	{ name: "settings", resources: ["settings.json"], policy: "never" },
	{ name: "mcp-never", resources: ["mcp.json"], policy: "never" },
	{ name: "mcp-ask-headless", resources: ["mcp.json"], policy: "ask" },
	{ name: "mcp-stored-denial", resources: ["mcp.json"], policy: "always", stored: false },
	{ name: "mcp-stored-approval", resources: ["mcp.json"], policy: "never", stored: true },
	{ name: "mcp-explicit-approval", resources: ["mcp.json"], policy: "never", override: true },
	{ name: "mcp-explicit-denial", resources: ["mcp.json"], policy: "always", override: false },
	{ name: "mcp-prompt-denial", resources: ["mcp.json"], policy: "ask", selection: "Do not trust (this session only)" },
	{ name: "mcp-prompt-approval", resources: ["mcp.json"], policy: "ask", selection: "Trust (this session only)" },
];
const directory = mkdtempSync(join(tmpdir(), "pier-project-trust-"));
const output = [];
try {
	for (const test of cases) {
		const cwd = join(directory, test.name, "project");
		const agentDir = join(directory, test.name, "agent");
		mkdirSync(join(cwd, ".pi"), { recursive: true });
		for (const resource of test.resources) {
			const content = resource === "mcp.json"
				? { mcpServers: { fixture: { command: "not-executed" } } } : {};
			writeFileSync(join(cwd, ".pi", resource), JSON.stringify(content));
		}
		const trustStore = new ProjectTrustStore(agentDir);
		if (test.stored !== undefined) trustStore.set(cwd, test.stored);
		let prompts = 0;
		const trusted = await resolveProjectTrusted({
			cwd, trustStore, defaultProjectTrust: test.policy, trustOverride: test.override,
			projectTrustContext: {
				hasUI: test.selection !== undefined,
				ui: { select: async () => { prompts++; return test.selection; } },
			},
		});
		const config = loadMcpConfig({ agentDir, cwd, projectTrusted: trusted });
		if (config.errors.length) throw new Error(JSON.stringify(config.errors));
		output.push({ ...test, expected: {
			hasResources: hasTrustRequiringProjectResources(cwd), trusted, prompts,
			servers: config.servers.length,
		} });
	}
} finally {
	rmSync(directory, { recursive: true, force: true });
}
console.log(JSON.stringify(output, null, 2));
