// Regenerate from repository root: node internal/highlight/testdata/js-classes/generate.mjs
// Authority: pi v1.4.0 (4a080252) pins highlight.js, whose langRe compiles
// JavaScript regexes. ECMAScript \s is WhiteSpace + LineTerminator, not Go's
// ASCII-only class.
import fs from 'node:fs';
import path from 'node:path';
import {createRequire} from 'node:module';
const root=process.cwd(),require=createRequire(import.meta.url);
const regex=[],highlights=[];
const probes=[['space',' '],['tab','\t'],['vt','\v'],['ff','\f'],['cr','\r'],['lf','\n'],['nbsp','\u00a0'],['ogham','\u1680'],['enquad','\u2000'],['hair','\u200a'],['ls','\u2028'],['ps','\u2029'],['nnbsp','\u202f'],['mmsp','\u205f'],['ideo','\u3000'],['bom','\ufeff'],['nel','\u0085'],['zero','\u200b'],['letter','a']];
for (const [source,name] of [[String.raw`\S`,'not-space'],[String.raw`\s`,'space-class'],[String.raw`^[^\s:]+$`,'not-space-colon'],[String.raw`[\s\S]`,'any-char'],[String.raw`[\w\s]+`,'word-or-space']]) {
  for (const [probe,input] of probes) {
    const match=new RegExp(source,'md').exec(input);
    const indices=match ? match.indices.flatMap(pair=>pair ? pair.map(i=>Buffer.byteLength(input.slice(0,i))) : [-1,-1]) : null;
    regex.push({name:name+'-'+probe,source,input,indices});
  }
}
const hljs=require(path.join(root,'pi/node_modules/highlight.js/lib/index.js'));
const cases=[
  ['ini','key\u00a0= true'],['ini','key\v= true'],['ini','key =\u00a0true'],
  ['javascript','const o = {a\u00a0: 1};'],['javascript','const o = {a\v: 1};'],
  ['xml','<a\u00a0href="x">y</a>'],['elixir',':\u00a0sym'],['armasm','\u00a0mov r0, r1'],
  ['pgsql','x\u00a0BETWEEN 1 AND 2'],['shell','echo hi\u00a0'],
  ['protobuf','x = 1;\u00a0'],['markdown','Title\n---\u00a0'],
  ['markdown','-\u00a0item'],['markdown','1.\u00a0item'],['markdown','-\vitem'],
  ['xml','<script\u00a0src="x">var a = 1;</script>'],['xml','<style\u00a0>a { color: red; }</style>'],
  ['xml','<script>y</script>'],['xml','<script\u00a0src="x">y</script>'],['xml','<style>y</style>'],
  ['markdown','```xml\n<script>y</script>\n```'],
  ['xml','<note id="x"y lang=\'en\'>'],['xml','<note id="😀"y lang=\'en\'>'],
  ['ruby','x = :\u00a0sym'],['vbnet','x = 1\nREM\u00a0comment'],
];
for (const [language,code] of cases) {
  highlights.push({name:'guard-'+highlights.length+'-'+language,language,code,html:hljs.highlight(code,{language}).value});
}
for (const [language,code] of [
  ['ini','; comment\u2028[section]\u2028key = true'],['ini','; comment\u2029[section]\u2029key = true'],['ini','; comment\u00a0x\nkey = true'],
]) {
  highlights.push({name:'ini-'+highlights.length,language,code,html:hljs.highlight(code,{language}).value});
}
const output=path.join(root,'internal/highlight/testdata/js-classes/fixtures.json');
fs.writeFileSync(output,JSON.stringify({regex,highlights},null,2)+'\n');
