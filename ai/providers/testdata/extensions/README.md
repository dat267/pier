# Extension provider catalogue fixture

`catalog.json` holds the two provider-extension catalogues this tree ports, so
`TestExtensionCatalogMatchesTheSources` can pin the Go catalogues against the
sources they were translated from.

The sources are the user's installed extensions, not `./pi`:

- `~/.pi/agent/extensions/providers/hyper/catalog.ts` (`CATALOG`)
- `~/.pi/agent/extensions/providers/commandcode/catalog.ts` (`CATALOG`), plus
  the `UNAVAILABLE_IDS` and `DOMINATED_IDS` sets, which together are
  `EXCLUDED_IDS`.

Regenerate after the extension sources change. From this directory:

```sh
node -e '
const fs = require("node:fs");
const dir = process.env.HOME + "/.pi/agent/extensions/providers";
function extractArray(src, name) {
  const start = src.indexOf("const " + name);
  const open = src.indexOf("= [", start) + 2;
  let depth = 0, i = open;
  for (; i < src.length; i++) {
    if (src[i] === "[") depth++;
    else if (src[i] === "]") { depth--; if (depth === 0) break; }
  }
  return src.slice(open, i + 1);
}
const catalogs = {};
for (const provider of ["hyper", "commandcode"]) {
  const src = fs.readFileSync(dir + "/" + provider + "/catalog.ts", "utf8");
  catalogs[provider] = JSON.parse(
    extractArray(src, "CATALOG")
      .replace(/(\d)_(?=\d)/g, "$1")
      .replace(/([{,]\s*)([A-Za-z_]\w*):/g, "\"$2\":")
      .replace(/,\s*\}/g, "}")
      .replace(/,\s*\]/g, "]"),
  );
}
const src = fs.readFileSync(dir + "/commandcode/catalog.ts", "utf8");
const excluded = new Set();
for (const name of ["UNAVAILABLE_IDS", "DOMINATED_IDS"]) {
  const start = src.indexOf("export const " + name);
  const open = src.indexOf("([", start) + 2;
  const close = src.indexOf("])", open);
  for (const match of src.slice(open, close).matchAll(/"([^"]+)"/g)) excluded.add(match[1]);
}
fs.writeFileSync("catalog.json", JSON.stringify({ catalogs, excluded: [...excluded] }, null, 1) + "\n");
'
```

The extraction is a text scan rather than an import because the catalogues are
TypeScript with type-only syntax and an `@earendil-works/pi-ai` import the
extensions resolve from their own install.
