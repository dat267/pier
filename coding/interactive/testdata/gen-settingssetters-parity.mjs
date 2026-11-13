// Reference: pi v1.5.2 (b4afdddf), core/settings-manager.ts setters and the file
// they persist: field order, dropped defaults and untouched initial keys.
import { mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync, existsSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { corpus, entries, emit, upstream } from "./golden-reference.mjs";
const { SettingsManager } = await upstream("coding-agent/src/core/settings-manager.ts");
const c = corpus("settingssetters");
const capitalize = (name) => name.charAt(0).toUpperCase() + name.slice(1);
for (const [label, s] of entries(c.cases)) {
	const dir = mkdtempSync(join(tmpdir(), "pier-settings-setter-"));
	try {
		const cwd = join(dir, "project");
		const agentDir = join(dir, "agent");
		mkdirSync(cwd, { recursive: true });
		mkdirSync(agentDir, { recursive: true });
		const settingsPath = join(agentDir, "settings.json");
		if (s.initial) writeFileSync(settingsPath, JSON.stringify(s.initial));
		const manager = SettingsManager.create(cwd, agentDir, { projectTrusted: true });
		for (const [name, value] of s.ops) {
			const setter = `set${capitalize(name)}`;
			if (typeof manager[setter] !== "function") throw new Error(`no ${setter}`);
			manager[setter](value === null ? undefined : value);
		}
		await manager.flush();
		emit(`x ${label}`, existsSync(settingsPath) ? readFileSync(settingsPath, "utf8") : null);
	} finally {
		rmSync(dir, { recursive: true, force: true });
	}
}
