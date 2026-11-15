// Reference: pi v1.5.2 (b4afdddf) packages/coding-agent/src/utils/changelog.ts
// normalizeChangelogLinks. Run from the repository root:
//   node --experimental-strip-types coding/testdata/changelog-links/generate.mjs ./pi > coding/testdata/changelog-links/golden.json
import { resolve } from "node:path";
import { pathToFileURL } from "node:url";

const root = resolve(process.argv[2] ?? "./pi");
const { normalizeChangelogLinks } = await import(
	pathToFileURL(resolve(root, "packages/coding-agent/src/utils/changelog.ts")).href
);

const cases = [
	["relative-file", "See [docs](docs/guide.md) and [code](../coding-agent/src/x.ts).", "1.2.3"],
	["dir-target", "See [dir](src/modes) and [trail](src/modes/).", "1.2.3"],
	["absolute", "[abs](/packages/coding-agent/src/a.ts)", "1.2.3"],
	["floating-ref", "[m](https://github.com/earendil-works/pi/blob/main/packages/coding-agent/src/a.ts)", "2.0.0"],
	["legacy-repo", "[m](https://github.com/badlogic/pi-mono/blob/master/x.md)", "2.0.0"],
	["external", "[e](https://example.com/a) [h](#section) [p](//cdn/x.js) [m](mailto:a@b.c)", "1.0.0"],
	["escape", "[e](docs/a b.md) and [i](img/a.png)", "1.0.0"],
	["traversal", "[t](../../outside.md)", "1.0.0"],
	["v-prefix", "[x](docs/a.md)", "v9.9.9"],
	["query-fragment", "[q](docs/a.md?x=1#frag)", "1.2.3"],
	["non-ascii", "[u](docs/é.md)", "1.2.3"],
	["dir-fragment", "[t](src/modes/#anchor)", "1.2.3"],
	["normalize", "[n](docs/./a/../b.md)", "1.2.3"],
	["escaped-percent", "[s](docs/a%20b.md)", "1.2.3"],
	["title", "[b](docs/a.md \"title\")", "1.2.3"],
	["traversal-fragment", "[h](../CHANGELOG.md#v1)", "1.2.3"],
	["at-sign", "[at](user@host:path)", "1.2.3"],
	["glob", "[st](docs/*.md)", "1.2.3"],
	["emoji", "[e](✨/x.md)", "1.2.3"],
	["plus", "[p](a+b/c.md)", "1.2.3"],
	["hash-only", "[h](#)", "1.2.3"],
	["dot-dot-root", "[d](/../x.md)", "1.2.3"],
	["drive", "[w](C:\\\\x\\\\y.md)", "1.2.3"],
	["opaque-scheme", "[o](javascript:alert(1))", "1.2.3"],
];

const fixtures = cases.map(([label, markdown, version]) => ({
	label,
	markdown,
	version,
	normalized: normalizeChangelogLinks(markdown, version),
}));
process.stdout.write(`${JSON.stringify(fixtures, null, "\t")}\n`);
