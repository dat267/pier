// Regenerate from the repository root:
// node coding/testdata/system-prompt/generate.mjs ./pi > coding/testdata/system-prompt/golden.json
// Reference: pi v1.4.0 (4a080252), packages/coding-agent/src/core/system-prompt.ts.
// Executes the pinned buildSystemPromptSections, never an installed bundle. The
// documented D176 `docs` section is dropped before recording.
import { resolve, join } from "node:path";
import { pathToFileURL } from "node:url";

const root = resolve(process.argv[2] ?? "./pi");
const { buildSystemPromptSections } = await import(
	pathToFileURL(join(root, "packages/coding-agent/src/core/system-prompt.ts")).href,
);

const skill = { name: "deploy", description: "Deploy <prod> & more", filePath: "/work/.pi/skills/deploy/SKILL.md" };
const cases = {
	"defaults": { cwd: "/work" },
	"empty-tools": { selectedTools: [], cwd: "/work" },
	"empty-tools-with-skills": { selectedTools: [], skills: [skill], cwd: "/work" },
	"read-only-skills": { selectedTools: ["read"], skills: [skill], cwd: "/work" },
	"bash-grep": {
		selectedTools: ["bash", "grep"], cwd: "/work",
		toolSnippets: { bash: "Run commands", grep: "Search contents" },
		toolGuidelines: { bash: ["Use bash wisely"], grep: ["Be exact"] },
	},
	"powershell-only": {
		selectedTools: ["powershell"], cwd: "/work",
		toolSnippets: { powershell: "Run PowerShell" }, toolGuidelines: { powershell: ["Use PS"] },
	},
	"prompt-guidelines": { selectedTools: ["read"], promptGuidelines: ["  Be concise ", "Be concise", "Extra rule"], cwd: "/work" },
	"custom-append-context": {
		customPrompt: "Custom system prompt.", appendSystemPrompt: "Appended text.",
		contextFiles: [{ path: "/work/AGENTS.md", content: "project rules" }], cwd: "/work",
	},
	"snippets-guidelines": {
		selectedTools: ["read", "bash", "edit"], cwd: "/work",
		toolSnippets: { read: "Read files", bash: "Run commands", edit: "Edit files" },
		toolGuidelines: { read: ["Read first"], bash: ["Run second"], edit: ["Edit third"] },
	},
	"cwd-windows": { cwd: "C:\\work\\proj" },
	"custom-section": { cwd: "/work", sections: { extra: "extra text" } },
};
const out = {};
for (const [name, input] of Object.entries(cases)) {
	const sections = buildSystemPromptSections(input);
	delete sections.docs;
	out[name] = { sections };
}
console.log(JSON.stringify(out, null, 2));
