// Extracts the LaTeX data tables from the pinned upstream and writes
// latex_tables.json, which latex_tables_test.go compares the Go maps with.
//
//	node --experimental-strip-types tui/testdata/gen_latex_tables.mjs > tui/testdata/latex_tables.json
//
// The tables are plain literals in pi/packages/tui/src/latex.ts; they are parsed
// from the text rather than imported because the module pulls in the renderer.
import { readFileSync } from "node:fs";

const source = readFileSync(new URL("../../pi/packages/tui/src/latex.ts", import.meta.url), "utf8");
const names = [
  "SYMBOLS", "NAMED_OPERATORS", "LIMIT_OPERATORS", "DISPLAY_LIMIT_SYMBOLS", "RELATION_COMMANDS",
  "NEGATED_SYMBOLS", "BLACKBOARD", "SUPERSCRIPTS", "SUBSCRIPTS", "SPACING_COMMANDS",
  "NEGATIVE_SPACING_COMMANDS", "FONT_SWITCH_COMMANDS", "IGNORED_COMMANDS", "SIZE_COMMANDS",
  "PLAIN_WRAPPERS", "ACCENTS",
];

function literal(name) {
  const start = source.indexOf("const " + name);
  if (start < 0) throw new Error("missing " + name);
  const equals = source.indexOf("=", start);
  const braces = source.indexOf("{", equals);
  const paren = source.indexOf("(", equals);
  const open = braces !== -1 && braces < source.indexOf("\n", equals) ? braces : paren;
  const closer = source[open] === "{" ? "}" : ")";
  let depth = 0;
  for (let i = open; i < source.length; i++) {
    if (source[i] === source[open]) depth++;
    else if (source[i] === closer) {
      depth--;
      if (depth === 0) return { kind: source[open] === "{" ? "map" : "set", body: source.slice(open, i + 1) };
    }
  }
  throw new Error("unterminated " + name);
}

const tables = {};
for (const name of names) {
  const { kind, body } = literal(name);
  if (kind === "set") {
    tables[name] = { kind, entries: [...body.matchAll(/"((?:[^"\\]|\\.)*)"/g)].map((m) => JSON.parse('"' + m[1] + '"')) };
    continue;
  }
  const entries = {};
  for (const m of body.matchAll(/(?:^|[{,])\s*(?:"((?:[^"\\]|\\.)*)"|([A-Za-z0-9_]+))\s*:\s*"((?:[^"\\]|\\.)*)"/g)) {
    entries[m[1] !== undefined ? JSON.parse('"' + m[1] + '"') : m[2]] = JSON.parse('"' + m[3] + '"');
  }
  tables[name] = { kind, entries };
}
process.stdout.write(JSON.stringify(tables, null, 1) + "\n");
