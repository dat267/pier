// Generates golden.json from WHATWG URL parsing (Node's `new URL`), which is
// what the reference's `URL.pathname` does while parsing. Paths starting with
// "//" are left out: the reference reads them as protocol-relative URLs with a
// host, a divergence the "https://h//x" D-row records.
const paths = [
	"/a/./b", "/a/../b", "/a/b/..", "/a/b/.", "/.", "/..", "/../a", "/a/../../b", "/a/%2e/b",
	"/a/%2E%2E/b", "/a/.%2e/b", "/a/%2e./b", "/a/..%2f/b", "/a/%2e%2e%2f/b", "/a/...", "/a/..b",
	"/a/%2e%2e", "/a/%2e%2e/", "/a/b/%2e", "/a//../b", "/a/..;/b", "/a/%2e%2e%2e/b",
];
const rows = paths.map((path) => ({ path, pathname: new URL(path, "https://h").pathname }));
process.stdout.write(`${JSON.stringify({ source: "WHATWG URL pathname", base: "https://h", rows }, null, "\t")}\n`);
