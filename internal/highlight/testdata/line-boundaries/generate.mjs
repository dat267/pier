// Regenerate from repository root: node internal/highlight/testdata/line-boundaries/generate.mjs
// Read-only authority: pi v1.4.0 (4a080252), tui's pinned highlight.js and
// ECMAScript RegExp semantics used by highlight.js lib/core.js langRe.
import fs from 'node:fs';
import path from 'node:path';
import {createRequire} from 'node:module';
const root=process.cwd(), require=createRequire(import.meta.url);
const hljs=require(path.join(root,'pi/node_modules/highlight.js/lib/index.js'));
const regex=[], highlights=[];
const add=(name,source,input,flags='m')=>{
  const match=new RegExp(source,flags+'d').exec(input);
  const indices=match ? match.indices.flatMap(pair=>pair ? pair.map(i=>Buffer.byteLength(input.slice(0,i))) : [-1,-1]) : null;
  regex.push({name,source,input,flags,indices});
};
for (const [name,line] of [['lf','\n'],['cr','\r'],['crlf','\r\n'],['ls','\u2028'],['ps','\u2029'],['nel','\u0085']]) {
  add(name+'-anchors','^foo$','before'+line+'foo'+line+'after');
  add(name+'-dot','(.+)','aa'+line+'bb');
  add(name+'-class',String.raw`([^\n]+)`,'aa'+line+'bb');
  add(name+'-empty','^^$$',line);
  add(name+'-captures','^(a+?)(a*)$','before'+line+'aaaa'+line+'after');
  add(name+'-dotall','^(.+)$','aa'+line+'bb','ms');
  for (const [language,code] of [
    ['go','// comment'+line+'package demo'],
    ['javascript','// comment'+line+'const value = 1;'],
    ['python','# comment'+line+'return True'],
    ['markdown','# Heading'+line+'## Next'],
    ['ini','; comment'+line+'[section]'+line+'key = true'],
    ['javascript','const value = "a'+line+'b";'],
    ['go','var caf\u00e9 = 1'+line+'// note'+line+'package demo'],
    ['javascript','const caf\u00e9 = "\u00e9";'+line+'// note'+line+'const next = 1;'],
    ['markdown','\ud83d\ude00'+line+'# Heading'+line+'## Next'],
  ]) highlights.push({name:name+'-'+language+'-'+highlights.length,language,code,html:hljs.highlight(code,{language}).value});
}
add('alternative-fallback','(^foo|foo)','xfoo\r');
add('alternative-at-line','(^foo|foo)','x\u2028foo');
add('literal-cr-before-anchor',String.raw`(\r)^foo$`,'before\rfoo');
add('literal-ls-before-anchor','(\u2028)^foo$','before\u2028foo');
add('lazy-literal-cr',String.raw`(.*?)(\r)(.*)`,'aa\rbb');
add('literal-cr',String.raw`(\r)`,'aa\rbb');
add('literal-ls','(\u2028)','aa\u2028bb');
add('greedy-alternative','(a+|ab)','\r aaab');
add('lazy-alternative','(a+?|ab)','\r aaab');
add('optional-capture','(a)?b','\r b');
add('multibyte-before-cr','\u00e9\r foo','x\u00e9\rfoo');
add('astral-before-ls','\ud83d\ude00\u2028foo','x\ud83d\ude00\u2028foo');
const output=process.argv[2]??path.join(root,'internal/highlight/testdata/line-boundaries/fixtures.json');
fs.writeFileSync(output,JSON.stringify({regex,highlights},null,2)+'\n');
