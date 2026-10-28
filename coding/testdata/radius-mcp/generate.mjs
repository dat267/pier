// Regenerate from the repository root (read-only upstream):
// node --experimental-strip-types coding/testdata/radius-mcp/generate.mjs ./pi > coding/testdata/radius-mcp/golden.json
// pi v1.4.0 (4a080252), interactive-mode.ts offerRadiusMcpServer and
// extensions/mcp/config.ts addMcpServerConfig. Execute the actual method with
// a minimal host instead of importing the interactive app and native helpers.
import { execFileSync } from "node:child_process";
import { stripTypeScriptTypes } from "node:module";
import * as path from "node:path";
import { pathToFileURL } from "node:url";
import { mkdtempSync, mkdirSync, writeFileSync, readFileSync, existsSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";

const root = path.resolve(process.argv[2] ?? "./pi");
if (!execFileSync("git", ["-C", root, "rev-parse", "HEAD"], { encoding: "utf8" }).startsWith("4a080252")) {
  throw new Error("requires pinned pi v1.4.0 (4a080252)");
}
const { loadMcpConfig, addMcpServerConfig } = await import(pathToFileURL(path.join(root, "packages/coding-agent/src/extensions/mcp/config.ts")));
const { DEFAULT_RADIUS_GATEWAY, normalizeRadiusGatewayUrl } = await import(pathToFileURL(path.join(root, "packages/ai/src/providers/radius-config.ts")));
const source = readFileSync(path.join(root, "packages/coding-agent/src/modes/interactive/interactive-mode.ts"), "utf8");
const start = source.indexOf("\tprivate offerRadiusMcpServer(");
const end = source.indexOf("\n\t// =========================================================================", start);
if (start < 0 || end < 0) throw new Error("offerRadiusMcpServer moved");
const method = stripTypeScriptTypes(source.slice(start, end).replace("private offerRadiusMcpServer", "function offerRadiusMcpServer"));
const createMethod = new Function("path", "getAgentDir", "loadMcpConfig", "RADIUS_MCP_URL", "ExtensionSelectorComponent", "addMcpServerConfig", method + "\nreturn offerRadiusMcpServer;");

const cases = [
  {name: "missing-yes", initial: null, choice: "Yes"},
  {name: "missing-no", initial: null, choice: "No"},
  {name: "missing-cancel", initial: null, choice: "cancel"},
  {name: "empty-object", initial: "{}", choice: "Yes"},
  {name: "matching-url-only", initial: '{"mcpServers":{"radius":{"url":"https://radius.pi.dev/mcp"}}}', choice: "Yes"},
  {name: "custom-config", initial: '{\n\t"vendor": "<keep>&",\n\t"mcpServers": {\n\t\t"tools": {"url":"https://radius.pi.dev/mcp///","headers":{"Z":"z","A":"a"},"oauth":{"clientId":"old"},"toolExposure":{"z*":"direct","*":"hidden"},"vendor":{"z":1,"a":2}}\n\t},\n\t"autoEnableCodemode": false\n}', choice: "Yes"},
  {name: "already-configured", initial: '{"mcpServers":{"tools":{"url":"https://radius.pi.dev/mcp/","auth":{"provider":"radius"}}}}', choice: "Yes"},
  {name: "different-provider", initial: '{"mcpServers":{"tools":{"url":"https://radius.pi.dev/mcp","auth":{"provider":"other"}}}}', choice: "Yes"},
  {name: "name-collision", initial: '{"mcpServers":{"radius":{"command":"unused"}}}', choice: "Yes"},
  {name: "both-names-used", initial: '{"mcpServers":{"radius":{"command":"unused"},"radius-mcp":{"command":"also-unused"}}}', choice: "Yes"},
  {name: "first-endpoint", initial: '{"mcpServers":{"first":{"url":"https://radius.pi.dev/mcp"},"second":{"url":"https://radius.pi.dev/mcp"}}}', choice: "Yes"},
  {name: "project-excluded", initial: null, project: '{"mcpServers":{"radius":{"url":"https://radius.pi.dev/mcp","auth":{"provider":"radius"}}}}', choice: "Yes"},
  {name: "bom", initial: '\uFEFF{\n    "mcpServers": {}\n}', choice: "Yes"},
  {name: "long-indent", initial: '{\n            "mcpServers": {}\n}', choice: "Yes"},
  {name: "truncated-object", initial: '{', choice: "Yes"},
  {name: "truncated-close", initial: '{"mcpServers":{}', choice: "Yes"},
  {name: "trailing-json", initial: '{"mcpServers":{}} {}', choice: "Yes"},
  {name: "duplicate-keys", initial: '{"mcpServers":{"radius":{"url":"https://radius.pi.dev/mcp"}},"note":1,"note":2}', choice: "Yes"},
  {name: "malformed", initial: '{broken', choice: "Yes"},
  {name: "null", initial: 'null', choice: "Yes"},
  {name: "array", initial: '[]', choice: "Yes"},
  {name: "bad-servers", initial: '{"mcpServers":null}', choice: "Yes"},
];
const results = cases.map((entry) => {
  const dir = mkdtempSync(path.join(tmpdir(), "pier-radius-mcp-"));
  try {
    const global = path.join(dir, "mcp.json");
    if (entry.initial !== null) writeFileSync(global, entry.initial);
    if (entry.project) {
      mkdirSync(path.join(dir, ".pi"));
      writeFileSync(path.join(dir, ".pi/mcp.json"), entry.project);
    }
    let selector, error = false, reloads = 0;
    class Selector {
      constructor(title, options, onSelect, onCancel) { selector = { onSelect, onCancel }; }
    }
    const offer = createMethod(path, () => dir, loadMcpConfig, `${normalizeRadiusGatewayUrl(DEFAULT_RADIUS_GATEWAY)}/mcp`, Selector, addMcpServerConfig);
    offer.call({
      sessionManager: {getCwd: () => dir},
      showSelector: (create) => create(() => {}),
      showError: () => { error = true; },
      handleReloadCommand: () => { reloads++; },
      ui: {requestRender() {}},
    }, "radius", "Radius");
    if (selector) {
      if (entry.choice === "cancel") selector.onCancel();
      else selector.onSelect(entry.choice);
    }
    return {...entry, offer: !!selector, error, reloads, expected: existsSync(global) ? readFileSync(global, "utf8") : null};
  } finally { rmSync(dir, {recursive: true, force: true}); }
});
process.stdout.write(JSON.stringify(results, null, 2) + "\n");
