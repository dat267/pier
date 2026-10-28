// Regenerate from repository root: node internal/highlight/testdata/line-terminators/generate.mjs
// Authority: pi v1.4.0 (4a080252) highlight.js compiles JavaScript regexes,
// whose multiline `$` matches before CR, LF, U+2028 and U+2029.
import fs from 'node:fs';
import path from 'node:path';
import {createRequire} from 'node:module';
const root=process.cwd(),require=createRequire(import.meta.url);
const hljs=require(path.join(root,'pi/node_modules/highlight.js/lib/index.js'));
const cases=[
  ['python-repl','>>>\u2028print(1)'],['python-repl','>>>\u2029print(1)'],
  ['python-repl','>>>\rprint(1)'],['python-repl','>>>\r\nprint(1)'],
  ['python-repl','...\u2028x'],['node-repl','>\u2028const x = 1'],
  ['node-repl','>\u2029const x = 1'],['node-repl','>\rconst x = 1'],
  ['yaml','name:\u2028demo'],['yaml','name:\u2029demo'],['yaml','name:\rdemo'],
  ['yaml','-\u2028item'],['yaml','-\ritem'],['yaml','name:\u00a0demo'],
  ['yaml','name:\u3000demo'],
];
const fixtures=cases.map(([language,code],index)=>({name:`case-${index}-${language}`,language,code,html:hljs.highlight(code,{language}).value}));
const output=path.join(root,'internal/highlight/testdata/line-terminators/fixtures.json');
fs.writeFileSync(output,JSON.stringify(fixtures,null,2)+'\n');
