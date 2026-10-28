// Regenerate from repository root: node internal/highlight/testdata/code-units/generate.mjs
// Read-only authority: pi v1.4.0 (4a080252), whose highlight.js compiles
// JavaScript regexes without /u, so matching advances by UTF-16 code units.
import fs from 'node:fs';
import path from 'node:path';
import {createRequire} from 'node:module';
const root=process.cwd(),require=createRequire(import.meta.url);
const hljs=require(path.join(root,'pi/node_modules/highlight.js/lib/index.js'));
const dir=path.join(root,'internal/highlight/testdata');
const manifest=JSON.parse(fs.readFileSync(path.join(dir,'golden_manifest.json'),'utf8'));
const highlights=[];
const emoji='😀';
for(const entry of manifest){
  if(entry.file==='sample_big.go') continue;
  const text=fs.readFileSync(path.join(dir,entry.file),'utf8');
  const line=text.split('\n').find(value=>value.trim().length>0);
  if(line===undefined) continue;
  const variants=[
    line+' '+emoji,
    emoji+line,
    line.replace(/"/,'"'+emoji),
    line.replace(/\\/,'\\'+emoji),
  ];
  for(const [index,code] of variants.entries()){
    highlights.push({name:`${entry.language}-${index}`,language:entry.language,code,html:hljs.highlight(code,{language:entry.language}).value});
  }
}
const units=[];
const probes=[
  ['.','😀'],[String.raw`^.$`,'😀'],[String.raw`^..$`,'😀'],[String.raw`(.)(.)`,'😀'],
  [String.raw`[\s\S]`,'😀'],[String.raw`[\s\S]{2}`,'😀'],[String.raw`\w`,'😀'],
  ['😀','😀'],[String.raw`[😀]`,'😀'],[String.raw`[^x]+`,'😀x'],
  [String.raw`a.c`,'a😀c'],[String.raw`^.+$`,'a😀b'],
  [String.raw`[a\Sb]`,'a'],[String.raw`[a\Sb]`,'\u00a0'],[String.raw`[a\Sb]`,'\u0085'],
  [String.raw`[a\Sb]`,'😀'],[String.raw`[\S]`,' '],[String.raw`[\S]`,'x'],
  [String.raw`[^\S]`,' '],[String.raw`[^\S]`,'\u00a0'],[String.raw`[^\S]`,'x'],
  [String.raw`[^\s\S]`,'x'],[String.raw`[\S\n]`,'\n'],[String.raw`[\S-]`,'-'],
];
for(const [source,input] of probes){
  const match=new RegExp(source,'md').exec(input);
  const indices=match?match.indices.flatMap(pair=>pair?[...pair]:[-1,-1]):null;
  units.push({name:`unit-${units.length}-${source}`,source,input,indices});
}
const output=path.join(root,'internal/highlight/testdata/code-units/fixtures.json');
fs.writeFileSync(output,JSON.stringify({units,highlights},null,2)+'\n');
