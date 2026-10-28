// Regenerate from repository root: node internal/highlight/testdata/sublanguage-continuations/generate.mjs
// Authority: pi v1.4.0 (4a080252) highlight.js _highlight reuses the previous
// sublanguage top mode, so quoted shell state continues across prompts.
import fs from 'node:fs';
import path from 'node:path';
import {createRequire} from 'node:module';
const root=process.cwd(),require=createRequire(import.meta.url);
const hljs=require(path.join(root,'pi/node_modules/highlight.js/lib/index.js'));
const cases=[
  ['shell','$ echo "a\n$ b"\n'],
  ['shell','$ echo "a\n# b"\n'],
  ['shell',"$ 'a\n$ b'\n"],
  ['shell','$ echo "a\n> b"\n'],
];
const fixtures=cases.map(([language,code],index)=>({name:`case-${index}-${language}`,language,code,html:hljs.highlight(code,{language}).value}));
fs.writeFileSync(path.join(root,'internal/highlight/testdata/sublanguage-continuations/fixtures.json'),JSON.stringify(fixtures,null,2)+'\n');
