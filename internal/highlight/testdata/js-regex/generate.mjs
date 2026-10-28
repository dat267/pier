// Regenerate from repository root: node internal/highlight/testdata/js-regex/generate.mjs
// Read-only authority: pi v1.4.0 (4a080252), whose highlight.js compiles
// JavaScript regexes. The snapshot covers every compilable pattern from
// testdata/compiled_patterns.json against synthetic Unicode inputs, plus the
// Annex B escapes, classes and quantifier forms that differ from Go's syntax.
import fs from 'node:fs';
import path from 'node:path';
import {createRequire} from 'node:module';
const root=process.cwd(),require=createRequire(import.meta.url);
const dir=path.join(root,'internal/highlight/testdata');

const edgeSources=[
  String.raw`\u{1F600}`,String.raw`[\u{1F600}]`,String.raw`\u{61}`,String.raw`\u0061`,
  String.raw`\uD83D`,String.raw`\uD83D\uDE00`,String.raw`\u12`,String.raw`\uZZZZ`,
  String.raw`\x41`,String.raw`\x{41}`,String.raw`\x4`,
  String.raw`[]`,String.raw`[^]`,String.raw`[]a]`,String.raw`[^]a]`,
  String.raw`[\]]`,String.raw`[\}]`,String.raw`[\[]`,
  String.raw`[a-\d]`,String.raw`[\d-a]`,String.raw`[\w-\s]`,String.raw`[\s-\S]`,
  String.raw`[a\-b]`,String.raw`[a-]`,String.raw`[-a]`,String.raw`[a&&b]`,
  String.raw`a{2}`,String.raw`a{2,}`,String.raw`a{2,3}`,String.raw`a{`,String.raw`a{2`,
  String.raw`a{2,1}`,String.raw`a{b}`,String.raw`a{1F600}`,String.raw`}`,String.raw`]`,
  String.raw`{`,String.raw`a]b`,String.raw`a}b`,String.raw`a{2}{3}`,
  String.raw`[\uD83D]`,String.raw`[\uD83D-\uDE00]`,String.raw`[^\uD800-\uDFFF]`,
  String.raw`[\u{1F600}\S]`,String.raw`[^\u{1F600}]`,String.raw`\u{2}{2}`,
];
const edgeInputs=['a','aa','u','u{61}','u{1F600}','x','A','{','}','1F600','2','2,3','b','-',']','[','😀','\u00a0','\n','ab','a{2}','\u2028'];

const unitIndices=(input,match)=>match?match.indices.flatMap(pair=>pair?[...pair]:[-1,-1]):null;

const cases=[];
for(const source of edgeSources){
  let re;
  try{ re=new RegExp(source,'md'); }catch(error){ continue; }
  cases.push({source,flags:'m',inputs:edgeInputs.map((input,index)=>({input,indices:unitIndices(input,re.exec(input))}))});
}

// Every compilable compiled pattern, against inputs that exercise Unicode
// terminals, ECMAScript whitespace and astral code units.
const snapshot=JSON.parse(fs.readFileSync(path.join(dir,'compiled_patterns.json'),'utf8'));
const looksUnsupported=source=>source.includes('(?=')||source.includes('(?!')||source.includes('(?<=')||source.includes('(?<!');
const hasBackreference=source=>/\\(?:[1-9])/.test(source);
const probeInputs=['a','_ 1:\u00a0😀\n'];
for(const [index,entry] of snapshot.entries()){
  if(looksUnsupported(entry.src)||hasBackreference(entry.src)) continue;
  const effective=[...new Set((entry.flags+'m').replace(/[gy]/g,'').split(''))].join('');
  let re;
  try{ re=new RegExp(entry.src,effective+'d'); }catch(error){ continue; }
  const inputs=[];
  for(const input of probeInputs){
    const indices=unitIndices(input,re.exec(input));
    if(indices!==null && indices.length>64) continue;
    inputs.push({input,indices});
  }
  cases.push({pattern:index,source:entry.src,flags:effective,inputs});
}

const output=path.join(dir,'js-regex','fixtures.json');
fs.writeFileSync(output,JSON.stringify(cases)+'\n');
console.log(cases.length);
