// Run from the repository root:
// node internal/highlight/testdata/ignored-illegals/benchmark.mjs
// Same inputs as BenchmarkIgnoredIllegalScanner, against pinned highlight.js.
import path from 'node:path';
import {createRequire} from 'node:module';
const require=createRequire(import.meta.url), root=process.cwd();
const hljs=require(path.join(root,'pi/node_modules/highlight.js/lib/core.js'));
for(const language of ['json','javascript','python']){
  hljs.registerLanguage(language,require(path.join(root,`pi/node_modules/highlight.js/lib/languages/${language}.js`)));
}
const json='{"key":"value","number":123},'.repeat(256);
const cases=[
  ['repeated','json','0x'.repeat(4096)],
  ['json','json',json],
  ['valid-json','json','['+json.slice(0,-1)+']'],
  ['javascript','javascript','const x = 42; // comment\n'.repeat(128)],
  ['python','python','def run(x):\n    return x + 42 # comment\n'.repeat(128)],
];
for(const [name,language,code] of cases){
  const run=()=>hljs.highlight(code,{language,ignoreIllegals:true});
  for(let i=0;i<50;i++)run();
  const samples=[];
  for(let n=0;n<5;n++){
    const start=performance.now();
    for(let i=0;i<200;i++)run();
    samples.push((performance.now()-start)/200);
  }
  const median=[...samples].sort((a,b)=>a-b)[2];
  console.log(`${name} (${code.length} bytes ASCII): ${samples.map(n=>n.toFixed(3)).join(', ')} ms/op, median ${median.toFixed(3)}`);
}
