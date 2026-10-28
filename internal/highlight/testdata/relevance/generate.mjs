// Regenerate from the repository root (read-only upstream):
//   node internal/highlight/testdata/relevance/generate.mjs > internal/highlight/testdata/relevance/golden.json
// pi v1.4.0 (4a080252), pi/node_modules/highlight.js. Each case pins HTML and
// the relevance score for a construct whose mode relevance the port had to get
// exactly right (see the CR-167 note in the test).
import fs from 'node:fs';
import path from 'node:path';
import {createRequire} from 'node:module';

const root = process.cwd();
const require = createRequire(import.meta.url);
const hljs = require(path.join(root, 'pi/node_modules/highlight.js/lib/index.js'));
const manifest = JSON.parse(fs.readFileSync(path.join(root, 'internal/highlight/testdata/golden_manifest.json'), 'utf8'));
const fileFor = (language) => manifest.find((entry) => entry.language === language)?.file;

const cases = [];
const add = (language, code) => {
  const result = hljs.highlight(code, { language, ignoreIllegals: false });
  cases.push({ name: `${language}-${cases.length}`, language, code, html: result.value, illegal: result.illegal === true, relevance: result.relevance });
};

// Semicolon separator comments carry relevance 0 (hsp, nsis).
add('hsp', '; a comment\nmes "hi"');
add('nsis', '; NSIS sample\nName "Demo"');
// Number wrappers spread core modes that carry relevance 0 (tcl).
add('tcl', 'set x 10');
add('tcl', 'set x $y');
// Zeroed wrappers whose variants inherit that relevance (fortran, lsl, ruby).
add('fortran', 'x = "a"');
add('fortran', "x = 'a'");
add('lsl', '// a comment');
add('lsl', '/* a block comment */');
add('ruby', ':value');
add('ruby', 'x = :v');
add('ruby', 'puts "a #{b} c"');
// Documentation comments and their inner doctags (csharp, kotlin, dart).
add('csharp', '/// <summary>A demo.</summary>');
add('csharp', '/// <remarks>More.</remarks>');
add('kotlin', '/** doc */\nfun x() {}');
add('kotlin', 'override fun run() {}');
// Function/params relevance 0 in the java family.
add('java', 'public void run() {');
add('java', 'void run()');
add('csharp', 'public int Add(int a) { return a; }');
add('csharp', 'class Box<T> where T : class {}');
// Attribute and tag internals (xml).
add('xml', '<note id="1" lang=\'en\'>x</note>');
add('xml', '<a b="c">d</a>');
// Markdown link parts and inner parts are zeroed.
add('markdown', '[link](https://example.com)');
add('markdown', '[ref][id]');
add('markdown', '# Heading\n\nSome **bold** and *italic*.');
// Selector wrappers (less) keep the wrapper zero and variants default.
add('less', '.box { color: red; }');
add('less', '@var: 1;');
// Keyword guards and decorators (javascript, typescript).
add('javascript', 'obj.x = $y;');
add('javascript', 'const re = /[a-z]+/gi;');
add('javascript', '@sealed class A {}');
add('typescript', 'const re: RegExp = /[a-z]+/gi;');
add('typescript', '@sealed class A {}');
add('typescript', 'const a: number = 1;');
// Perl variable variants.
add('perl', 'my $q = qq{x};');
add('perl', '$q');
add('perl', 'say $name;');
// Crystal string and regexp wrappers.
add('crystal', 'require "json"');
add('crystal', 'x = %r(a)');
add('crystal', "x = 'a'");
// Whole-fixture controls so the corpus also covers ordinary code.
// Whole-fixture controls: every language whose relevance this corpus pins.
for (const language of ['hsp', 'nsis', 'tcl', 'fortran', 'lsl', 'ruby', 'csharp', 'kotlin', 'java', 'xml', 'markdown', 'less', 'javascript', 'typescript', 'perl', 'erlang', 'crystal']) {
  const file = fileFor(language);
  if (!file) continue;
  add(language, fs.readFileSync(path.join(root, 'internal/highlight/testdata', file), 'utf8'));
}
// Variant expansion inherits the wrapper's contains, class, keywords and
// sub-language (core.js inherit); nested emphasis, perl interpolation and the
// JSDoc space eater are the cases that needed it.
add('markdown', 'd** and *italic*');
add('markdown', 'Some **bold** and *italic* and `code`.');
add('perl', 'my $interp = "name=$name count=$count";');
add('perl', 'my $qq = qq{interpolated $name};');
add('ruby', "        heredoc #{@value} text");
add('javascript', '/**\n * @param {string} name the name\n * @returns {Demo}\n */');
add('javascript', '/**\n * @retur\u2028ns {Demo}\n */');
add('erlang', '-module(main).\n-export([main/0, add/2]).');
add('dart', '/// See [the docs](https://dart.dev).');
process.stdout.write(JSON.stringify(cases, null, 1) + '\n');
