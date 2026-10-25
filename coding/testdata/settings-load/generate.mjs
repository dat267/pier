// Regenerate from the repository root:
// node coding/testdata/settings-load/generate.mjs ./pi > coding/testdata/settings-load/golden.json
// Reference: pi v1.4.0 (4a080252), packages/coding-agent/src/core/settings-manager.ts.
// Executes the pinned SettingsManager over temp settings.json files, never an
// installed bundle. Getter values that can echo a wrongly typed member
// (compactionEnabled, thinkingBudgets) are deliberately not recorded; the
// retained members are asserted through the typed getters instead.
import { resolve, join } from "node:path";
import { pathToFileURL } from "node:url";
import { mkdtempSync, mkdirSync, writeFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";

const root = resolve(process.argv[2] ?? "./pi");
const { SettingsManager } = await import(
	pathToFileURL(join(root, "packages/coding-agent/src/core/settings-manager.ts")).href,
);

const defaults = { theme: "dark", steeringMode: "all", transport: "websocket", editorPaddingX: 3,
	compaction: { enabled: false, reserveTokens: 12000 }, retry: { maxRetries: 5, baseDelayMs: 250 },
	defaultTools: ["read", "bash"] };
const cases = {
	"valid-baseline": { global: defaults },
	"global-theme-number": { global: { ...defaults, theme: 5 } },
	"global-theme-array": { global: { ...defaults, theme: ["dark"] } },
	"global-compaction-enabled-string": { global: { ...defaults, compaction: { enabled: "yes", reserveTokens: 12000 } } },
	"project-theme-number": {
		global: { ...defaults, steeringMode: "one-at-a-time" },
		project: { theme: 5, steeringMode: "all", editorPaddingX: 7 },
	},
	"project-compaction-enabled-string": {
		global: defaults,
		project: { compaction: { enabled: "yes", keepRecentTokens: 5000 } },
	},
	"invalid-json": { globalRaw: "{\"theme\": \"dark\"," },
	"unknown-key": { global: { ...defaults, xUnknown: { a: 1 } } },
};

const out = {};
for (const [name, test] of Object.entries(cases)) {
	const agentDir = mkdtempSync(join(tmpdir(), "pier-settings-"));
	const cwd = mkdtempSync(join(tmpdir(), "pier-settings-cwd-"));
	try {
		writeFileSync(join(agentDir, "settings.json"), test.globalRaw ?? JSON.stringify(test.global));
		if (test.projectRaw !== undefined || test.project !== undefined) {
			mkdirSync(join(cwd, ".pi"), { recursive: true });
			writeFileSync(join(cwd, ".pi", "settings.json"), test.projectRaw ?? JSON.stringify(test.project));
		}
		const manager = SettingsManager.create(cwd, agentDir, { projectTrusted: true });
		out[name] = {
			getters: {
				steeringMode: manager.getSteeringMode(),
				theme: manager.getThemeSetting() ?? null,
				transport: manager.getTransport(),
				defaultTools: manager.getDefaultTools() ?? null,
				editorPaddingX: manager.getEditorPaddingX(),
				compactionReserveTokens: manager.getCompactionReserveTokens(),
				retry: manager.getRetrySettings(),
			},
		};
	} finally {
		rmSync(agentDir, { recursive: true, force: true });
		rmSync(cwd, { recursive: true, force: true });
	}
}
console.log(JSON.stringify(out, null, 2));
