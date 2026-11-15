// Dumps the reference's parseArgs result for a fixed argv corpus.
//
// Usage: node --experimental-strip-types generate.mjs <path-to-pi-checkout>
//
// Reference: pi v1.5.2 (b4afdddf) packages/coding-agent/src/cli/args.ts
// parseArgs. The corpus is hand-written to cover every branch: value flags with
// and without a value, a value that starts with - or @, -- handling, repeated
// flags, the list flags' comma splitting, and unknown-flag forms (--f, --f=v,
// --f v, -x).
import { resolve } from "node:path";
import { pathToFileURL } from "node:url";

const root = resolve(process.argv[2] ?? "./pi");
const { parseArgs } = await import(
	pathToFileURL(resolve(root, "packages/coding-agent/src/cli/args.ts")).href
);

const corpus = [
	[],
	["hi"],
	["@file.txt"],
	["hi", "@a", "there"],
	["--"],
	["--", "hi"],
	["--", "--model", "x"],
	["--", "@a"],
	["--help"],
	["-h"],
	["--version"],
	["-v"],
	["--mode", "text"],
	["--mode", "json"],
	["--mode", "rpc"],
	["--mode", "xml"],
	["--mode"],
	["--mode", "-c"],
	["--continue"],
	["-c"],
	["--resume"],
	["-r"],
	["--provider", "p"],
	["--provider"],
	["--provider", "-x"],
	["--provider", "@f"],
	["--model", "m"],
	["--model"],
	["--model", "@f"],
	["--api-key", "k"],
	["--api-key"],
	["--system-prompt", "s"],
	["--system-prompt"],
	["--append-system-prompt", "a"],
	["--append-system-prompt", "a", "--append-system-prompt", "b"],
	["--append-system-prompt"],
	["--name", "n"],
	["-n", "n"],
	["--name"],
	["--no-session"],
	["--session", "s"],
	["--session"],
	["--session-id", "i"],
	["--session-id"],
	["--fork", "f"],
	["--fork"],
	["--session-dir", "d"],
	["--session-dir"],
	["--models", "a,b"],
	["--models", "a,,b,"],
	["--models", "-x"],
	["--models", "---x"],
	["--models", "@f"],
	["--models"],
	["-nt"],
	["-nbt"],
	["-t", "a,b"],
	["-t"],
	["-xt", "a"],
	["-xt"],
	["--thinking", "high"],
	["--thinking", "bogus"],
	["--thinking"],
	["-p"],
	["--export", "f"],
	["--export"],
	["-e", "x"],
	["-ne"],
	["--no-mcp"],
	["--skill", "s"],
	["--prompt-template", "p"],
	["--theme", "t"],
	["--use-theme", "u"],
	["--use-theme"],
	["-ns"],
	["-np"],
	["--no-themes"],
	["-nc"],
	["--list-models"],
	["--list-models", "all"],
	["--list-models", "@f"],
	["--list-models", "-x"],
	["--tui-mode", "fullscreen"],
	["--tui-mode"],
	["--tui-mode", "-x"],
	["--verbose"],
	["-a"],
	["-na"],
	["--offline"],
	["--foo"],
	["--foo", "bar"],
	["--foo", "-x"],
	["--foo", "@f"],
	["--foo=bar"],
	["--foo="],
	["-x"],
	["-xyz"],
	["---x"],
	["--foo", "bar", "--baz", "qux"],
	["--model", "m", "--provider", "p", "hi", "@f"],
	["-c", "-r"],
	["--session-id", "i", "--continue"],
	["--mode", "rpc", "--offline", "--verbose", "prompt"],
];

// Seeded random argv vectors: combinations of the same tokens, so flag
// interactions the curated list does not spell out (a repeated flag, a value
// that is itself a flag, -- in the middle) are covered too. The seed is fixed
// so the golden stays byte for byte.
const tokens = [
	"--mode",
	"text",
	"json",
	"rpc",
	"--model",
	"m",
	"--provider",
	"--api-key",
	"-c",
	"-r",
	"--no-session",
	"--session",
	"s",
	"--session-id",
	"--fork",
	"--models",
	"a,b",
	"-t",
	"-xt",
	"-nt",
	"--thinking",
	"high",
	"-p",
	"--export",
	"-e",
	"-ne",
	"--no-mcp",
	"--skill",
	"--theme",
	"--use-theme",
	"--list-models",
	"--tui-mode",
	"--verbose",
	"-a",
	"-na",
	"--offline",
	"--",
	"@f",
	"hi",
	"--foo",
	"--foo=bar",
	"-x",
];

let seed = 20261115;
function random() {
	// mulberry32: deterministic and good enough to mix token positions.
	seed = (seed + 0x6d2b79f5) | 0;
	let t = seed;
	t = Math.imul(t ^ (t >>> 15), t | 1);
	t ^= t + Math.imul(t ^ (t >>> 7), t | 61);
	return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
}

for (let index = 0; index < 120; index++) {
	const length = Math.floor(random() * 7);
	const argv = [];
	for (let token = 0; token < length; token++) {
		argv.push(tokens[Math.floor(random() * tokens.length)]);
	}
	corpus.push(argv);
}

// Only the fields pier ports, and only when set: JS undefined and an absent Go
// nil/empty value must compare equal.
function project(args) {
	const out = {};
	for (const key of [
		"provider",
		"model",
		"apiKey",
		"systemPrompt",
		"thinking",
		"mode",
		"name",
		"session",
		"sessionId",
		"fork",
		"sessionDir",
		"useTheme",
		"tuiMode",
		"export",
	]) {
		if (args[key] !== undefined) out[key] = args[key];
	}
	for (const key of [
		"continue",
		"resume",
		"help",
		"version",
		"noSession",
		"noTools",
		"noBuiltinTools",
		"noExtensions",
		"noMcp",
		"print",
		"noSkills",
		"noPromptTemplates",
		"noThemes",
		"noContextFiles",
		"offline",
		"verbose",
	]) {
		if (args[key]) out[key] = true;
	}
	for (const key of [
		"appendSystemPrompt",
		"models",
		"tools",
		"excludeTools",
		"extensions",
		"skills",
		"promptTemplates",
		"themes",
		"messages",
		"fileArgs",
	]) {
		if (args[key] && args[key].length > 0) out[key] = args[key];
	}
	if (args.listModels !== undefined) out.listModels = args.listModels;
	if (args.projectTrustOverride !== undefined) out.projectTrustOverride = args.projectTrustOverride;
	if (args.unknownFlags && args.unknownFlags.size > 0) {
		out.unknownFlags = Object.fromEntries(args.unknownFlags);
	}
	if (args.diagnostics && args.diagnostics.length > 0) {
		out.diagnostics = args.diagnostics.map((d) => `${d.type}:${d.message}`);
	}
	return out;
}

const fixtures = corpus.map((argv) => ({ argv, result: project(parseArgs(argv)) }));
console.log(JSON.stringify(fixtures, null, 1));
