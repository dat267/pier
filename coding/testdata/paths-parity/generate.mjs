// Generates paths_cases.json from the pinned pi's own utils/paths.ts.
// The corpus freezes a home directory and explicit bases so the rows do not
// depend on the machine; skip `cwdRelative` for ~ inputs, whose expansion reads
// the real home directory. Malformed file:// URLs are left out: upstream throws
// and pier returns the input unchanged (D-row "An invalid file:// URL is
// returned unchanged instead of raising"), which that row's tests already pin.
import { join, resolve } from "node:path";
import { pathToFileURL } from "node:url";

const pin = resolve(process.argv[2] ?? join(import.meta.dirname, "../../../pi"));
const {
	getCwdRelativePath,
	isPathWithin,
	normalizePath,
	pathsEqual,
	resolvePath,
	shortenHomePath,
} = await import(pathToFileURL(join(pin, "packages/coding-agent/src/utils/paths.ts")).href);

const HOME = "/home/tester";
const BASE = "/base/dir";

const inputs = [
	"", " ", ".", "..", "~", "~/", "~/x", "~/x/", "~//x", "~x", "@file", "@", "@/abs/x",
	"/abs/x", "rel", "./rel/", "a/../../b", "file:///tmp/x", "file:///tmp/a%20b",
	"\\\\server\\share", "C:\\x", "a\u202Fb", "Cafe\u0301", "Caf\u00e9", "a' b", "x7 AM.png",
];

const cases = inputs.map((input) => {
	const row = { input };
	row.normalize = normalizePath(input, { homeDir: HOME });
	row.trimmed = normalizePath(input, { trim: true, homeDir: HOME });
	row.spaces = normalizePath(input, { normalizeUnicodeSpaces: true, stripAtPrefix: true, homeDir: HOME });
	row.resolve = resolvePath(input, BASE, { homeDir: HOME });
	row.noTilde = normalizePath(input, { expandTilde: false, homeDir: HOME });
	if (!input.startsWith("~")) {
		const relative = getCwdRelativePath(input, BASE);
		if (relative !== undefined) row.cwdRelative = relative;
	}
	return row;
});

const comparisons = [];
for (const [parent, child] of [
	["/a/b", "/a/b"], ["/a/b", "/a/b/c"], ["/a/b", "/a/bc"], ["/a/b/", "/a/b/c"], ["/a/b", "/a"],
	["/a/b", "/a/b/../c"], ["/", "/x"], ["/a/b", "relative"], ["/a/b", "/a/b/"], [".", "./x"],
]) {
	comparisons.push({ parent, child, within: isPathWithin(parent, child), equal: pathsEqual(parent, child) });
}

const shortened = [];
for (const path of ["/home/tester/x", "/home/tester", "/home/testerx", "rel", "/home/tester/x/y"]) {
	shortened.push({ path, home: HOME, result: shortenHomePath(path, HOME) });
}

const fixture = {
	// upstream: pi v1.5.2 (b4afdddf) packages/coding-agent/src/utils/paths.ts
	source: "utils/paths.ts",
	home: HOME,
	base: BASE,
	cases,
	comparisons,
	shortened,
};
process.stdout.write(`${JSON.stringify(fixture, null, "\t")}\n`);
