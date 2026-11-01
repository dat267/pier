// Regenerates the jsdiff parity fixture from the pinned package.
// Regenerate from the repository root:
//   node internal/jsdiff/testdata/parity/generate.mjs ./pi > internal/jsdiff/testdata/parity/cases.json
// Reference: pi v1.4.0 (4a080252) node_modules/diff (jsdiff 8.x), the library
// packages/coding-agent/src/core/tools/edit-diff.ts calls (D87). Deterministic:
// the pseudo-random stream is seeded in this file.
import { createRequire } from "node:module";
import { join, resolve } from "node:path";

const root = resolve(process.argv[2] ?? "./pi");
const require = createRequire(join(root, "package.json"));
const Diff = require("diff");

let seed = 105;
const rand = () => (seed = (seed * 1103515245 + 12345) & 0x7fffffff) / 0x7fffffff;
const pick = (n) => Math.floor(rand() * n);

const cases = [];
const run = (oldArr, newArr) => ({
  old: oldArr,
  new: newArr,
  parts: Diff.diffArrays(oldArr, newArr).map((part) => ({
    count: part.count,
    added: Boolean(part.added),
    removed: Boolean(part.removed),
  })),
});
// Duplicate-heavy and repeat-heavy sequences stress the diagonal pruning.
for (let i = 0; i < 160; i++) {
  const alphabet = ["a", "b", "c", "d"].slice(0, 1 + pick(4));
  const make = (n) => Array.from({ length: n }, () => alphabet[pick(alphabet.length)]);
  cases.push(run(make(pick(12)), make(pick(12))));
}
for (const [oldArr, newArr] of [
  [[], []],
  [[], ["a"]],
  [["a"], []],
  [["a"], ["a"]],
  [["a"], ["b"]],
  [["a", "b"], ["b", "a"]],
  [["a", "a", "a"], ["a"]],
  [["a"], ["a", "a", "a"]],
  [["a", "b", "c", "d"], ["d", "c", "b", "a"]],
  [["a", "b", "c"], ["a", "c"]],
  [["a", "c"], ["a", "b", "c"]],
]) {
  cases.push(run(oldArr, newArr));
}
process.stdout.write(JSON.stringify(cases));
