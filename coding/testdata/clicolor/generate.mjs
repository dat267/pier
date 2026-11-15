// Reference: pi v1.5.2 (b4afdddf) prints CLI diagnostics with chalk, whose
// color decision is the pinned supports-color's createSupportsColor. This
// generator records that library's answer for a corpus of environments, argv
// searches and stream states, so coding/clicolor.go is compared with the
// library rather than with a hand-written table. Run from the repository root:
//   node coding/testdata/clicolor/generate.mjs ./pi > coding/testdata/clicolor/golden.json
import { createRequire } from "node:module";
import { pathToFileURL } from "node:url";
import { join, resolve } from "node:path";

const root = resolve(process.argv[2] ?? "./pi");
const require = createRequire(join(root, "package.json"));
// supports-color caches the FORCE_COLOR level it saw first in module state, so
// every case loads a fresh module instance through a cache-busting query.
const modulePath = pathToFileURL(require.resolve("supports-color")).href;
let instance = 0;
const loadSupportsColor = async () => (await import(`${modulePath}?case=${instance++}`)).createSupportsColor;

const envCases = [
	{},
	{ TERM: "dumb" },
	{ TERM: "dumb", FORCE_COLOR: "1" },
	{ CI: "true", GITHUB_ACTIONS: "1" },
	{ CI: "true", GITEA_ACTIONS: "1" },
	{ CI: "true", CIRCLECI: "1" },
	{ CI: "true", TRAVIS: "1" },
	{ CI: "true", APPVEYOR: "1" },
	{ CI: "true", GITLAB_CI: "1" },
	{ CI: "true", BUILDKITE: "1" },
	{ CI: "true", DRONE: "1" },
	{ CI: "true", CI_NAME: "codeship" },
	{ CI: "true" },
	{ CI: "true", FORCE_COLOR: "1" },
	{ TF_BUILD: "True", AGENT_NAME: "hosted" },
	{ TF_BUILD: "True" },
	{ TEAMCITY_VERSION: "9.0" },
	{ TEAMCITY_VERSION: "9.00" },
	{ TEAMCITY_VERSION: "9.1.2" },
	{ TEAMCITY_VERSION: "9.01." },
	{ TEAMCITY_VERSION: "9" },
	{ TEAMCITY_VERSION: "10." },
	{ TEAMCITY_VERSION: "8.1" },
	{ TEAMCITY_VERSION: "100" },
	{ TERM: "xterm-256color" },
	{ TERM: "xterm-256" },
	{ TERM: "weird-256" },
	{ TERM: "myscreen" },
	{ TERM: "screen" },
	{ TERM: "screen-256color" },
	{ TERM: "xterm" },
	{ TERM: "XTERM" },
	{ TERM: "vt100" },
	{ TERM: "vt220" },
	{ TERM: "rxvt-unicode" },
	{ TERM: "cygwin" },
	{ TERM: "linux" },
	{ TERM: "ansi" },
	{ TERM: "foo-color" },
	{ TERM: "foo" },
	{ TERM: "xterm-kitty" },
	{ TERM: "xterm-ghostty" },
	{ TERM: "wezterm" },
	{ COLORTERM: "truecolor" },
	{ COLORTERM: "" },
	{ COLORTERM: "yes" },
	{ TERM_PROGRAM: "iTerm.app", TERM_PROGRAM_VERSION: "3.4.0" },
	{ TERM_PROGRAM: "iTerm.app", TERM_PROGRAM_VERSION: "2.9" },
	{ TERM_PROGRAM: "Apple_Terminal" },
	{ TERM_PROGRAM: "vscode" },
	{ FORCE_COLOR: "0" },
	{ FORCE_COLOR: "false" },
	{ FORCE_COLOR: "" },
	{ FORCE_COLOR: "2abc" },
	{ FORCE_COLOR: "abc" },
	{ FORCE_COLOR: "-1" },
	{ FORCE_COLOR: "9" },
	{ FORCE_COLOR: "3" },
	{ FORCE_COLOR: "1", TERM: "dumb" },
	{ NO_COLOR: "1", TERM: "xterm" },
];
const argCases = [
	[],
	["--no-color"],
	["--no-colors"],
	["--color=false"],
	["--color=never"],
	["--color"],
	["--colors"],
	["--color=true"],
	["--color=always"],
	["--color=16m"],
	["--color=full"],
	["--color=truecolor"],
	["--color=256"],
	["--color", "--no-color"],
	["--no-color", "--color"],
	["--", "--color"],
	["--color", "--", "--no-color"],
];
const env = process.env;
const base = { ...env };
const platforms = ["linux", "win32"];
const cases = [];
const collect = async () => {
for (const envCase of envCases) {
	for (const isTTY of [true, false]) {
		for (const platform of platforms) {
			for (const key of Object.keys(env)) if (!(key in base)) delete env[key];
			for (const key of Object.keys(base)) if (!(key in envCase)) delete env[key];
			Object.assign(env, envCase);
			process.argv = ["node", "pi", ""];
			Object.defineProperty(process, "platform", { value: platform, configurable: true });
			const createSupportsColor = await loadSupportsColor();
			const level = createSupportsColor({ isTTY }, { sniffFlags: true });
			cases.push({
				env: envCase,
				isTTY,
				args: [],
				goos: platform,
				level: typeof level === "object" && level !== null ? level.level : level,
				colored: Boolean(level && (typeof level !== "object" || level.level > 0)),
			});
		}
	}
}
// argv flags only matter on a terminal or with a forced level, so pair the
// interesting argument lists with a few environments.
for (const envCase of [{ TERM: "xterm" }, {}, { CI: "true" }, { FORCE_COLOR: "0", TERM: "xterm" }]) {
	for (const args of argCases) {
		for (const key of Object.keys(env)) delete env[key];
		Object.assign(env, envCase);
		process.argv = ["node", "pi", ...args];
		Object.defineProperty(process, "platform", { value: "linux", configurable: true });
		const createSupportsColor = await loadSupportsColor();
		const level = createSupportsColor({ isTTY: true }, { sniffFlags: true });
		cases.push({
			env: envCase,
			isTTY: true,
			args,
			goos: "linux",
			level: typeof level === "object" && level !== null ? level.level : level,
			colored: Boolean(level && (typeof level !== "object" || level.level > 0)),
		});
	}
}
process.argv = ["node", "pi"];
Object.defineProperty(process, "platform", { value: process.platform, configurable: true });
process.stdout.write(`${JSON.stringify(cases, null, "\t")}\n`);
};
await collect();
