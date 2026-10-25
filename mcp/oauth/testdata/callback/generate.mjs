// Reference: pi v1.4.0 (4a080252), packages/mcp/src/oauth/callback.ts.
// Regenerate from repository root:
// node mcp/oauth/testdata/callback/generate.mjs ./pi > mcp/oauth/testdata/callback/golden.json
import { resolve, join } from "node:path";
import { pathToFileURL } from "node:url";

const root = resolve(process.argv[2] ?? "./pi");
const { OAuthCallbackServer } = await import(
	pathToFileURL(join(root, "packages/mcp/src/oauth/callback.ts")).href,
);
const cases = [
	{ name: "ipv4", options: { host: "127.0.0.1" }, requestPath: "/callback" },
	{ name: "ipv6", options: { host: "::1" }, requestPath: "/callback" },
	{ name: "unknown-path", options: {}, requestPath: "/other" },
	{ name: "escaped-path", options: { path: "/callback%2Fcustom" }, requestPath: "/callback%2Fcustom" },
	{ name: "encoded-alias", options: {}, requestPath: "/call%62ack" },
	{ name: "extra-path", options: { extraPaths: ["/other"] }, requestPath: "/other" },
	{ name: "escaped-pending", options: { path: "/callback%2Fcustom" }, requestPath: "/callback%2Fcustom?state=fixture-state&code=fixture-code", expectedPath: "/callback%2Fcustom" },
	{ name: "extra-escaped-pending", options: { extraPaths: ["/other%2Fcallback"] }, requestPath: "/other%2Fcallback?state=fixture-state&code=fixture-code", expectedPath: "/other%2Fcallback" },
	{ name: "wrong-pending-path", options: { extraPaths: ["/other"] }, requestPath: "/other?state=fixture-state&code=fixture-code", expectedPath: "/callback" },
];
const output = [];
for (const test of cases) {
	const server = await OAuthCallbackServer.listen(test.options);
	const pending = test.expectedPath === undefined ? undefined : server.waitForCallback("fixture-state", test.expectedPath).then(
		(callback) => ({ callback }),
		(error) => ({ error: error.message }),
	);
	let expected;
	try {
		const response = await fetch(new URL(test.requestPath, server.redirectUrl));
		expected = { status: response.status, body: await response.text() };
	} finally {
		await server.close();
	}
	if (pending) Object.assign(expected, await pending);
	output.push({ ...test, expected });
}
console.log(JSON.stringify(output, null, 2));
