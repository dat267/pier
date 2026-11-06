// Reference: pi v1.5.0 (f9a26365), core/skills.ts formatSkillsForPrompt for the
// three file-read tools and a skill with disable-model-invocation.
// Regenerate from the repository root:
// node --experimental-strip-types coding/testdata/skills-prompt/generate.mjs ./pi > coding/testdata/skills-prompt/golden.json
import { join, resolve, dirname } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";
const root = resolve(process.argv[2] ?? "./pi");
const here = dirname(fileURLToPath(import.meta.url));
const mod = await import(pathToFileURL(join(root, "packages/coding-agent/src/core/skills.ts")));
const result = mod.loadSkillsFromDir({ dir: here, source: "test" });
const prompts = {};
for (const tool of ["read", "bash", "indirect"]) {
  prompts[tool] = mod.formatSkillsForPrompt(result.skills, tool).split(here).join("<dir>");
}
console.log(JSON.stringify({
  skills: result.skills.map(skill => ({ name: skill.name, description: skill.description, disableModelInvocation: skill.disableModelInvocation ?? false })),
  prompts,
}, null, 2));
