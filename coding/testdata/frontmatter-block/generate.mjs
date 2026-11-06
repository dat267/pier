// Reference: pi v1.5.0 (f9a26365), utils/frontmatter.ts parses the frontmatter
// with the `yaml` package. Cases the package rejects are skipped: pier parses
// invalid YAML differently on purpose (see D380).
// Regenerate from the repository root:
// node --experimental-strip-types coding/testdata/frontmatter-block/generate.mjs ./pi > coding/testdata/frontmatter-block/golden.json
import { join, resolve } from "node:path";
import { pathToFileURL } from "node:url";
const root = resolve(process.argv[2] ?? "./pi");
const { parseFrontmatter } = await import(pathToFileURL(join(root, "packages/coding-agent/src/utils/frontmatter.ts")));
const body = "Body\n";
const block = indicator => `---\ndescription: ${indicator}\n  line one\n  line two\n---\n${body}`;
const cases = [];
const add = (name, content) => cases.push({ name, content });
for (const indicator of ["|", "|-", "|+", ">", ">-", ">+"]) {
  add(`${indicator} two lines`, block(indicator));
  add(`${indicator} one line`, `---\ndescription: ${indicator}\n  one\n---\n${body}`);
  add(`${indicator} blank line`, `---\ndescription: ${indicator}\n  a\n\n  b\n---\n${body}`);
  add(`${indicator} trailing blank`, `---\ndescription: ${indicator}\n  a\n\n---\n${body}`);
  add(`${indicator} trailing blanks`, `---\ndescription: ${indicator}\n  a\n\n\n---\n${body}`);
  add(`${indicator} empty`, `---\ndescription: ${indicator}\nname: n\n---\n${body}`);
  add(`${indicator} then key`, `---\ndescription: ${indicator}\n  a\n  b\nname: n\n---\n${body}`);
  add(`${indicator} trailing spaces`, `---\ndescription: ${indicator}\n  a   \n  b\n---\n${body}`);
  add(`${indicator} dashes`, `---\ndescription: ${indicator}\n  ---\n  b\n---\n${body}`);
  add(`${indicator} deep lines`, `---\ndescription: ${indicator}\n  a\n    deeper\n  c\n---\n${body}`);
  add(`${indicator} bool text`, `---\nflag: ${indicator}\n  true\n---\n${body}`);
  add(`${indicator} number text`, `---\ncount: ${indicator}\n  42\n---\n${body}`);
  add(`${indicator} crlf`, `---\r\ndescription: ${indicator}\r\n  a\r\n  b\r\n---\r\n${body}`);
  add(`${indicator} comment`, `---\ndescription: ${indicator} # note\n  a\n---\n${body}`);
  add(`${indicator} indent digit`, `---\ndescription: ${indicator}2\n    four spent\n---\n${body}`);
  add(`${indicator} before other key`, `---\nname: n\ndescription: ${indicator}\n  a\nargument-hint: "<x>"\n---\n${body}`);
  add(`${indicator} leading blank`, `---\ndescription: ${indicator}\n\n  a\n---\n${body}`);
  add(`${indicator} unicode`, `---\ndescription: ${indicator}\n  héllo 😀\n  wörld\n---\n${body}`);
}
add("indicator order digit strip", `---\ndescription: |2-\n    a\n    b\n---\n${body}`);
add("indicator order strip digit", `---\ndescription: |-2\n    a\n---\n${body}`);
add("folded explicit indent", `---\ndescription: >2\n    a\n    b\n---\n${body}`);
add("block of only blanks", `---\ndescription: |\n\n\n---\n${body}`);
add("block with comment line inside", `---\ndescription: |\n  # not a comment\n  b\n---\n${body}`);
add("two block scalars", `---\nname: |\n  n\ndescription: |\n  d\n---\n${body}`);
add("block at end without newline", `---\ndescription: |\n  a`);
add("space-only line inside", `---\ndescription: |\n  a\n   \n  b\n---\n${body}`);
add("folded trailing spaces", `---\ndescription: >\n  a   \n  b\n---\n${body}`);
add("folded more indented then blank then normal", `---\ndescription: >\n  a\n   b\n\n  c\n---\n${body}`);
add("folded leading blank", `---\ndescription: >\n\n  a\n  b\n---\n${body}`);
add("literal tab inside content", `---\ndescription: |\n  a\tb\n---\n${body}`);
add("indent digit one", `---\ndescription: |1\n a\n---\n${body}`);
add("unknown indicator suffix", `---\ndescription: |x\n  a\n---\n${body}`);
add("block then blank then key", `---\ndescription: |\n  a\n\nname: n\n---\n${body}`);
add("header trailing spaces", `---\ndescription: |   \n  a\n---\n${body}`);
add("empty block at delimiter", `---\ndescription: |\n---\n${body}`);
add("space line then deeper line", `---\ndescription: |\n  a\n     \n  b\n---\n${body}`);
add("folded clip trailing blank", `---\ndescription: >\n  a\n\n---\n${body}`);
add("keep content trailing spaces", `---\ndescription: |+\n  a   \n---\n${body}`);
add("folded three blanks inside", `---\ndescription: >\n  a\n\n\n  b\n---\n${body}`);
add("folded more indented first line", `---\ndescription: >\n  a\n    b\n  c\n---\n${body}`);
add("indent digit one content", `---\ndescription: |1\n a\n b\n---\n${body}`);
add("bom before frontmatter", `\ufeff---\ndescription: |\n  a\n---\n${body}`);
add("cr only line endings in block", `---\ndescription: |\r  a\r  b\r---\r${body}`);
add("folded comment lines", `---\ndescription: >\n  # one\n  # two\n---\n${body}`);
add("unicode combining", `---\ndescription: |\n  e\u0301cole\n  ok\n---\n${body}`);
add("block after empty key", `---\ndescription:\nname: |\n  n\n---\n${body}`);
add("block scalar then empty value", `---\ndescription: |\n  a\nargument-hint:\n---\n${body}`);
add("literal with trailing tab", `---\ndescription: |\n  a\t\n  b\n---\n${body}`);
add("folded single word per line", `---\ndescription: >\n  one\n  two\n  three\n---\n${body}`);
add("keep two trailing blank lines", `---\ndescription: |+\n  a\n\n\n---\n${body}`);
add("strip leading and trailing blanks", `---\ndescription: |-\n\n  a\n\n---\n${body}`);
add("plain scalar with pipe", `---\ndescription: a | b\n---\n${body}`);
add("quoted pipe", `---\ndescription: "|"\n---\n${body}`);
add("quoted greater", `---\ndescription: ">"\n---\n${body}`);
add("empty value", `---\ndescription:\n---\n${body}`);
add("no frontmatter", body);
const report = [];
for (const testCase of cases) {
  let parsed;
  try {
    parsed = parseFrontmatter(testCase.content);
  } catch {
    continue; // Invalid YAML: pier's subset parser does not report YAML errors (D380).
  }
  const strings = {};
  for (const [key, value] of Object.entries(parsed.frontmatter ?? {})) {
    if (typeof value === "string") strings[key] = value;
  }
  report.push({ name: testCase.name, content: testCase.content, strings, body: parsed.body });
}
console.log(JSON.stringify(report, null, 2));
