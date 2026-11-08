// Regenerate from the repository root:
// node --experimental-strip-types coding/testdata/auth-command/generate.mjs ./pi > coding/testdata/auth-command/golden.json
// Reference: pi v1.5.0 (f9a26365), packages/coding-agent/src/cli/auth-command.ts
// `parseAuthCommand`. It decides whether an invocation prints a credential or
// only checks readiness, so its parse is a trust-relevant surface.
import { registerHooks } from "node:module";
import { resolve } from "node:path";
import { pathToFileURL } from "node:url";

const root = resolve(process.argv[2] ?? "./pi");
registerHooks({
	resolve(specifier, context, next) {
		const map = {
			"@earendil-works/pi-telemetry": "packages/telemetry/src/index.ts",
			"@earendil-works/pi-ai": "packages/ai/src/index.ts",
			"@earendil-works/pi-ai/compat": "packages/ai/src/compat.ts",
			"@earendil-works/pi-schema": "packages/schema/src/index.ts",
			"@earendil-works/pi-schema/formats": "packages/schema/src/formats/index.ts",
			"@earendil-works/pi-agent-core": "packages/agent/src/index.ts",
		};
		return map[specifier]
			? { url: pathToFileURL(resolve(root, map[specifier])).href, shortCircuit: true }
			: next(specifier, context);
	},
});
const { parseAuthCommand } = await import(
	pathToFileURL(resolve(root, "packages/coding-agent/src/cli/auth-command.ts"))
);

const cases = [
	[],
	["auth"],
	["auth", "help"],
	["auth", "nope"],
	["auth", "check"],
	["auth", "check", "--json"],
	["auth", "check", "--credentials", "--no-refresh"],
	["auth", "check", "--json", "--json"],
	["auth", "check", "--provider", "anthropic"],
	["auth", "print-api-key"],
	["auth", "print-api-key", "--json"],
	["auth", "print-api-key", "--model", "x"],
	["auth", "print-bearer-token"],
	["auth", "print-bearer-token", "--min-expiry", "30m"],
	["auth", "print-bearer-token", "--min-expiry", "30M"],
	["auth", "print-bearer-token", "--min-expiry", "500ms"],
	["auth", "print-bearer-token", "--min-expiry", "2h"],
	["auth", "print-bearer-token", "--min-expiry", "00030m"],
	["auth", "print-bearer-token", "--min-expiry", "1"],
	["auth", "print-bearer-token", "--min-expiry"],
	["auth", "print-bearer-token", "--min-expiry", ""],
	["auth", "print-bearer-token", "--min-expiry", " 30m"],
	["auth", "print-bearer-token", "--min-expiry", "30m", "--json"],
	["auth", "print-bearer-token", "--min-expiry", "30ms", "--min-expiry", "1h"],
	["auth", "check", "--min-expiry", "30m"],
	["auth", "check", "--credentials", "--min-expiry", "30m"],
	["auth", "check", "-h"],
	["auth", "check", "--", "x"],
	["--provider", "auth", "check"],
	["Auth", "check"],
	["auth", "CHECK"],
	["auth", "check", ""],
	["auth", "print-bearer-token", "--min-expiry", "30s", "stray"],
];

const fixtures = [];
for (const args of cases) {
	let result;
	try {
		const parsed = parseAuthCommand(args);
		result = parsed === undefined ? null : {
			kind: parsed.kind,
			args: parsed.args,
			json: parsed.json,
			credentials: parsed.credentials,
			noRefresh: parsed.noRefresh,
			...(parsed.minExpiryMs === undefined ? {} : { minExpiryMs: parsed.minExpiryMs }),
		};
	} catch (error) {
		result = { error: error.message };
	}
	fixtures.push({ args, result });
}
process.stdout.write(`${JSON.stringify(fixtures, null, "\t")}\n`);
