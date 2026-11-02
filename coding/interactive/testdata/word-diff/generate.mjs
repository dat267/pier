// Regenerate from the repository root:
// node coding/interactive/testdata/word-diff/generate.mjs
// Authority: pi v1.5.0 (f9a26365) components/diff.ts using jsdiff 8.0.4
// diffWords, the implementation the port mirrors.
import fs from 'node:fs';
import path from 'node:path';
import {createRequire} from 'node:module';
const root = process.cwd();
const {diffWords} = createRequire(path.join(root, 'pi/package.json'))('diff');
const alphabet = [
  'a','b','c','Z','0','1','_','-','.',',','!','?','/',':',';','"',"'",'`',
  ' ','\t','\n','\u00a0','\u3000','\ufeff','\u0085','\u200b','\u00ad',
  'é','中','😀','\u0301','\u2003',
];
let state = 0x5eed1234;
const random = () => ((state = (Math.imul(state, 1664525) + 1013904223) >>> 0) / 2 ** 32);
const word = () => {
  let value = '';
  const length = 1 + Math.floor(random() * 40);
  for (let i = 0; i < length; i++) value += alphabet[Math.floor(random() * alphabet.length)];
  return value;
};
const cases = [];
for (let i = 0; i < 300; i++) {
  const oldText = word();
  // Half the cases mutate an insertion/deletion point; half are independent.
  let newText;
  if (i % 2 === 0) {
    const cut = Math.floor(random() * (oldText.length + 1));
    newText = oldText.slice(0, cut) + word().slice(0, 1 + Math.floor(random() * 10)) + oldText.slice(cut);
  } else {
    newText = word();
  }
  const changes = diffWords(oldText, newText).map((change) => ({
    value: change.value, added: !!change.added, removed: !!change.removed,
  }));
  cases.push({old: oldText, new: newText, changes});
}
fs.writeFileSync(path.join(root, 'coding/interactive/testdata/word-diff/fixtures.json'), JSON.stringify(cases, null, 1) + '\n');
console.log(`generated ${cases.length} word-diff fixtures`);
