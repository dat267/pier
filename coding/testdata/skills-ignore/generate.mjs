// Regenerate from the repository root:
// node coding/testdata/skills-ignore/generate.mjs ./pi > coding/testdata/skills-ignore/golden.json
// Reference: pi v1.4.0 (4a080252), core/skills.ts and its pinned ignore package.
import { resolve, join, dirname } from "node:path";
import { pathToFileURL } from "node:url";
import { mkdtempSync, mkdirSync, writeFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
const root = resolve(process.argv[2]);
const { loadSkillsFromDir } = await import(pathToFileURL(join(root, "packages/coding-agent/src/core/skills.ts")));
const skills = [
 { path: "blocked/SKILL.md", name: "blocked-root" },
 { path: "nested/blocked/SKILL.md", name: "blocked-nested" },
 { path: "nested/deeper/blocked/SKILL.md", name: "blocked-deep" },
 { path: "nested/visible/SKILL.md", name: "visible" },
 { path: "root.md", name: "root-file" },
];
const skillFiles = Object.fromEntries(skills.map(skill => [skill.path, `---\nname: ${skill.name}\ndescription: A fixture skill\n---\nInstructions.\n`]));
const scenarios = [
 ["basename-directory", "blocked/\n"],
 ["basename-file", "SKILL.md\n"],
 ["explicit-path", "nested/blocked/\n"],
 ["leading-slash", "/blocked/\n"],
 ["file-negation", "SKILL.md\n!nested/visible/SKILL.md\n"],
 ["pruned-directory-negation", "blocked/\n!nested/blocked/SKILL.md\n"],
 ["globstar", "**/blocked/\n"],
];
const inputs = [];
for (const ignoreFile of [".gitignore", ".ignore", ".fdignore"]) {
 for (const [name, rules] of scenarios) inputs.push({ name: `${ignoreFile}/${name}`, ignoreFiles: { [ignoreFile]: rules } });
 inputs.push({ name: `${ignoreFile}/nested-prefix`, ignoreFiles: { [`nested/${ignoreFile}`]: "blocked/\n" } });
}
const cases = [];
for (const scenario of inputs) {
 const directory = mkdtempSync(join(tmpdir(), "pier-skills-ignore-"));
 try {
  for (const [path, content] of Object.entries({ ...skillFiles, ...scenario.ignoreFiles })) {
   const fullPath = join(directory, path);
   mkdirSync(dirname(fullPath), { recursive: true });
   writeFileSync(fullPath, content);
  }
  const result = loadSkillsFromDir({ dir: directory, source: "path" });
  if (result.diagnostics.length > 0) throw Error(JSON.stringify(result.diagnostics));
  cases.push({ ...scenario, names: result.skills.map(skill => skill.name) });
 } finally { rmSync(directory, { recursive: true, force: true }); }
}
console.log(JSON.stringify({ skillFiles, allNames: skills.map(skill => skill.name), cases }, null, 2));
