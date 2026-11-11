// Reference: pi v1.5.0 (f9a26365), packages/coding-agent/src/cli/args.ts parseArgs.
// Regenerate from the repository root:
// node --experimental-strip-types coding/testdata/cli-args/generate.mjs ./pi > coding/testdata/cli-args/golden.json
import { registerHooks } from "node:module";
import { resolve } from "node:path";
import { pathToFileURL } from "node:url";
const root = resolve(process.argv[2] ?? "./pi");
registerHooks({ resolve(specifier, context, next) {
	const map = {
		"@earendil-works/pi-telemetry": "packages/telemetry/src/index.ts",
		"@earendil-works/pi-agent-core": "packages/agent/src/index.ts",
		"@earendil-works/pi-ai": "packages/ai/src/index.ts",
		"@earendil-works/pi-tui": "packages/tui/src/index.ts",
	};
	return map[specifier] ? { url: pathToFileURL(resolve(root, map[specifier])).href, shortCircuit: true } : next(specifier, context);
} });
const { parseArgs } = await import(pathToFileURL(resolve(root, "packages/coding-agent/src/cli/args.ts")));

const vectors = [
	[], ["--help"], ["-h"], ["--version"], ["-v"], ["--mode"], ["--mode", "json"], ["--mode", "bogus"],
	["--mode", "--print"], ["--continue"], ["-c"], ["--resume"], ["-r"], ["--provider", "anthropic"],
	["--provider"], ["--model", "claude-opus-4-5"], ["--api-key", "k"], ["--system-prompt", "sp"],
	["--append-system-prompt", "a"], ["--append-system-prompt", "a", "--append-system-prompt", "b"],
	["--name", "n"], ["-n", "n"], ["--name"], ["--name", "   "], ["--no-session"], ["--session", "id"],
	["--session-id", "uuid"], ["--fork", "id"], ["--session-dir", "/tmp/d"], ["--models", "a,b"],
	["--models", "a, b ,,"], ["--models", "all"], ["--tools", "read,bash"], ["--exclude-tools", "bash"],
	["--no-tools"], ["--no-builtin-tools"], ["--no-extensions"], ["--no-mcp"], ["--print"], ["--export", "x"],
	["--no-skills"], ["--skills", "s1,s2"], ["--prompt-templates", "p"], ["--no-prompt-templates"],
	["--themes", "t"], ["--use-theme", "dark"], ["--no-themes"], ["--no-context-files"], ["--offline"],
	["--tui-mode", "full"], ["--tui-mode", "bogus"], ["--verbose"], ["--approve"], ["--no-approve"],
	["--thinking", "high"], ["--thinking", "bogus"], ["--thinking", "HIGH"],
	["--", "hello", "@f.txt"], ["hello"], ["@f.txt"], ["@a", "@b"], ["--", "--help"],
	["--unknown", "value"], ["--unknown=value"], ["--unknown"], ["--print", "--mode", "rpc"],
	["--session", "a", "--resume"], ["--session-id", "x", "--session", "y"], ["--fork", "a", "--resume"],
	["--offline", "--print", "--model", "m"], ["--", ""], [""], ["--model", "-x"],
	["--list-models"], ["--list-models", "anthropic"], ["--models", ""], ["--tools", ""],
	["--exclude-tools", "a, b"], ["--skills", "a, b"], ["--provider=anthropic"],
	["--model=claude"], ["-n"], ["--session-dir"], ["--fork"], ["--thinking", "off"],
	["--approve", "false"], ["--no-approve", "--approve"], ["--print", "--"],
	["--mode", "text", "--mode", "json"], ["--export"], ["--use-theme"],
	["--tools", "read", "--tools", "bash"], ["--no-tools", "--tools", "read"],
	// Empty values, and values that look like flags.
	["--model="], ["--mode="], ["--tools="], ["--thinking="], ["--name="], ["--models="],
	["--thinking", ""], ["--append-system-prompt", ""], ["--list-models", ""],
	["--name", "--"], ["--export", "--"], ["--session-dir", "--"], ["--name", "-p"],
	["--model", "--model", "x"], ["--session-id", ""], ["--session-id", "bad/id"],
	["--no-session=1"], ["-nt="],
	// Positional and message rules around --print and --.
	["--print", "foo", "bar"], ["-p", "foo", "bar"], ["-p", "---x"], ["-p", "--", "x"],
	["-p", "--", "--"], ["-pc"], ["-rc"], ["-cr"], ["-nt", "--", "x"],
	["--", "--", "x"], ["--", "-p", "x"], ["@"], ["@@x"], ["@--x"], ["--", "@"], ["--", "@@y"],
	// Lists are consumed once, never twice.
	["--tools", "a", "b"], ["--models", "a", "b"], ["--skills", "a", "b"], ["--themes", "a", "b"],
	["--prompt-templates", "a", "b"], ["--exclude-tools", "a", "b"],
	// Trust pair, unicode, and a listing flag next to a filter flag.
	["-a"], ["-a", "--no-approve"], ["--approve", "--no-approve"],
	["héllo"], ["--name", "héllo"], ["-p", "héllo", "@f"],
	["--list-models", "--offline"], ["--list-models", "@x"], ["--offline", "--list-models-all", "--verbose"],
];
const projection = (args) => {
	const p = (v) => (v === undefined ? null : v);
	return {
		provider: p(args.provider), model: p(args.model), apiKey: p(args.apiKey),
		systemPrompt: p(args.systemPrompt), appendSystemPrompt: args.appendSystemPrompt ?? [],
		thinking: p(args.thinking), continue: !!args.continue, resume: !!args.resume,
		help: !!args.help, version: !!args.version, mode: p(args.mode), name: p(args.name),
		noSession: !!args.noSession, session: p(args.session), sessionId: p(args.sessionId),
		fork: p(args.fork), sessionDir: p(args.sessionDir), models: args.models ?? [],
		tools: args.tools ?? [], excludeTools: args.excludeTools ?? [], noTools: !!args.noTools,
		noBuiltinTools: !!args.noBuiltinTools, noExtensions: !!args.noExtensions, noMcp: !!args.noMcp,
		print: !!args.print, export: p(args.export), noSkills: !!args.noSkills, skills: args.skills ?? [],
		promptTemplates: args.promptTemplates ?? [], noPromptTemplates: !!args.noPromptTemplates,
		themes: args.themes ?? [], useTheme: p(args.useTheme), noThemes: !!args.noThemes,
		noContextFiles: !!args.noContextFiles, listModels: p(args.listModels), offline: !!args.offline,
		tuiMode: p(args.tuiMode), verbose: !!args.verbose,
		projectTrustOverride: p(args.projectTrustOverride),
		messages: args.messages, fileArgs: args.fileArgs,
		unknownFlags: Array.from(args.unknownFlags.entries()).sort(([a], [b]) => (a < b ? -1 : a > b ? 1 : 0)).map(([flag, value]) => [flag, value]),
		diagnostics: args.diagnostics.map((d) => ({ type: d.type, message: d.message })),
	};
};
console.log(JSON.stringify(vectors.map((argv) => ({ argv, expected: projection(parseArgs(argv)) })), null, 2));
