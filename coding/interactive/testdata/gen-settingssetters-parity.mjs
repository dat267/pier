// Reference: pi v1.5.2 (b4afdddf), core/settings-manager.ts setters and the files
// they persist, global and project scope: field order, dropped values and
// untouched initial keys.
import { existsSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { corpus, entries, emit, upstream } from "./golden-reference.mjs";
const { SettingsManager } = await upstream("coding-agent/src/core/settings-manager.ts");
const c = corpus("settingssetters");
const capitalize = (name) => name.charAt(0).toUpperCase() + name.slice(1);
const read = (path) => (existsSync(path) ? readFileSync(path, "utf8") : null);
for (const [label, s] of entries(c.cases)) {
	const dir = mkdtempSync(join(tmpdir(), "pier-settings-setter-"));
	try {
		const cwd = join(dir, "project");
		const agentDir = join(dir, "agent");
		mkdirSync(join(cwd, ".pi"), { recursive: true });
		mkdirSync(agentDir, { recursive: true });
		const globalPath = join(agentDir, "settings.json");
		const projectPath = join(cwd, ".pi", "settings.json");
		if (s.initial) writeFileSync(globalPath, JSON.stringify(s.initial));
		const manager = SettingsManager.create(cwd, agentDir, { projectTrusted: true });
		for (const [scope, name, value] of s.ops) {
			const setter = scope === "project" ? `setProject${capitalize(name)}` : `set${capitalize(name)}`;
			if (typeof manager[setter] !== "function") throw new Error(`no ${setter}`);
			manager[setter](value === null ? undefined : value);
		}
		await manager.flush();
		emit(`x ${label}`, { global: read(globalPath), project: read(projectPath) });
	} finally {
		rmSync(dir, { recursive: true, force: true });
	}
}
