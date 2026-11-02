// Regenerate from the repository root:
// node internal/highlight/testdata/ignored-illegals/generate.mjs
// Authority: pi v1.5.0 (f9a26365), src/utils/syntax-highlight.ts,
// highlight.js 10.7.3. The custom modes exercise its core scanner contracts.
import fs from 'node:fs';
import path from 'node:path';
import {createRequire} from 'node:module';
const root=process.cwd(), require=createRequire(import.meta.url);
const hljs=require(path.join(root,'pi/node_modules/highlight.js/lib/core.js'));
hljs.registerLanguage('json',require(path.join(root,'pi/node_modules/highlight.js/lib/languages/json.js')));
hljs.registerLanguage('batch-boundaries',()=>({
  illegal:/\S/, keywords:'true false', contains:[
    {className:'first',begin:/ab|b/},
    {className:'number',begin:/\b\d+/},
    {className:'line',begin:/^@/},
    {className:'nested',begin:/\[/,end:/\]/,illegal:/\S/,contains:[
      {className:'string',begin:/"/,end:/"/},
      {className:'number',begin:/\b\d+/},
    ]},
  ],
}));
hljs.registerLanguage('batch-multi',()=>({illegal:/[xy]+/,contains:[{className:'hit',begin:/yZ/}]}));
hljs.registerLanguage('batch-zero',()=>({contains:[{begin:/</,end:/>/,illegal:/(?:)/,contains:[{className:'hit',begin:/ok/}]}]}));
hljs.registerLanguage('batch-callback',()=>({illegal:/\S/,contains:[{
  className:'hit',begin:/(T\d+)/,end:/>/,
  'on:begin':(m,r)=>{if(m[1]==='T0')r.ignoreMatch();},
  'on:end':(m,r)=>{if(m.input[m.index-1]==='!')r.ignoreMatch();},
}]}));
hljs.registerLanguage('batch-fold',()=>({case_insensitive:true,illegal:/[a-z]/,contains:[{className:'number',begin:/\d+/}]}));
// Go uses a manual matcher for this begin, so batching must remain disabled.
hljs.registerLanguage('batch-manual',()=>({illegal:/\S/,contains:[{className:'hit',begin:/@/,end:/;/}]}));
hljs.registerLanguage('batch-embedded',()=>({contains:[{begin:/</,end:/>/,subLanguage:'json',illegal:/\S/}]}));

const cases=[];
const add=(language,code)=>{for(const ignoreIllegals of [false,true])cases.push({language,code,ignoreIllegals});};
for(const code of [
  '', 'x', 'xx', 'x x ', 'xxxx 123 true false null', '0x'.repeat(64),
  'xx0x0x !! 12 0xff -1.2e+3 true', 'xx0x0x"text"xx0x0x',
  '[xx xx "value", 123, true, {"x": null} ]',
  '{xx xx "key":xx xx [true,false,null,123], xx"after":2}',
  'xx xx // a comment\r\n123', 'xx xx /* a comment */ 123',
  'xx xx "escaped \\\" quote" true', 'xx xx "unfinished\n',
  'xx xx 123\u00a0null\u2028"string"', 'xx xx \u2029[1,2]',
  'xx xx \0\x1b<&> "<&>"', 'xx xx 中文 😀 \u0301 123',
  '[xx xx]', '[xx xx\r\n]', '{xx xx}', '[xx xx [xx xx] xx xx]',
]) add('json',code);
for(const code of [
  'xx xx 0xx 123 true false ab b', 'xxx\r@xx\n@yy\u2028@zz\u2029@',
  '!! !! abc ab b', 'xx xx [xx xx "value" xx xx 42] true',
  'xx xx[xx xx[xx xx]xx xx] 123', '😀😀 中文\u00a0!!123',
]) add('batch-boundaries',code);
for(const code of ['xxxyZ','x xyZ yZ','xxxxxyZ']) add('batch-multi',code);
for(const code of ['<xxok>','<😀ok>','<okxxok>','<xx\r\nok>']) add('batch-zero',code);
for(const code of ['!! !!T0??T1 value>', '!! !!T0T1!>value> xx xx T2>', 'xx xx T2>xx xx']) add('batch-callback',code);
for(const code of ['aaZZ 123 éÉ', 'aaZZ kKK ssſ 123', 'aaZZ00aaZZ']) add('batch-fold',code);
for(const code of ['xx xx @value; xx', 'xx xx @😀; @more;', 'xx xx\r\n@value;']) add('batch-manual',code);
for(const code of ['<xx xx 0x0x true>', '<xx xx ["value"><xx xx ,123]>', '<xx xx "unfinished><xx xx quote">']) add('batch-embedded',code);

// Deterministic malformed JSON fragments cover boundaries throughout nested modes.
let state=0x31415926;
const fragments=['xx xx','0x0x','12','true','false','null','"value"',':',',','[',']','{','}','\r\n','\u2028','😀','/*x*/','//x\n','\\','"'];
for(let i=0;i<180;i++){
  let code='';
  for(let n=0;n<10;n++){
    state=(Math.imul(state,1664525)+1013904223)>>>0;
    code+=fragments[state%fragments.length];
  }
  add('json',code);
}
const fixtures=cases.map((c,i)=>{
  const r=hljs.highlight(c.code,{language:c.language,ignoreIllegals:c.ignoreIllegals});
  return {name:`case-${i}-${c.language}`,...c,html:r.value,relevance:r.relevance,illegal:r.illegal};
});
fs.writeFileSync(path.join(root,'internal/highlight/testdata/ignored-illegals/fixtures.json'),JSON.stringify(fixtures,null,2)+'\n');
console.log(`generated ${fixtures.length} ignored-illegal scanner fixtures`);
