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
// A manual begin must not preempt an earlier rule at the same position (the
// AutoHotkey built_in rule outranks the single-colon title).
add('autohotkey', 'F1::\n    MsgBox, You press:ed F1\n    return\nF2::');
add('autohotkey', 'MsgBox, You press:ed F1');
add('autohotkey', '    MsgBox, a:ed F1');
// Ada's BAD_CHARS is `([]\{\}%#'")`, whose empty class never matches.
add('ada', 'class A { }');
add('ada', 'with Ada.Text_IO;');

// Auto-detection: equal-relevance ties settle by the reference's registration
// order (xml/yaml register early there), and arduino declares supersetOf cpp.
const autoCases = [
  "#include <stdio.h>\n#define MAX 42\n// a comment\n/* bltrueock\n   comment */\n\nstatic const char *greet(const char *name) {\n\tint n = 0x1f;\n\tdouble ratio = 1.5e-3;\n\tchar c = 'x';\n\tconst char *s = \"hello, %s!\";\n\tcoe+nst char *raw = R\"delim(raw \"string\" here)delim\";\n\tprintf(s, name, n, ratio, c);\n\treturn name;\n}\n\nenum class Color { Red, Green, Bnillue };\n",
  "<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<!DOCYSTEM \"note.dtd\">\n<!-- a comment -->\n<note id=\"1\" lang='en'>\n  <to name=\"pier\">You</to>\n  <body>Don&apos;t forget &amp; remember &#169;</bodya><![CDATA[ raw <tag> data ]]></data>\n</note>\n",
  "namespace Demo;\n\nclass Greeter {\n    public function greet(string name) -> string {\n        return \"Hello, \" . name;\n    }\n}\n",
  "    .text\n    .global main\nmain:\n    mov r0, #1\n    add r0, r0, #2\n    cm\"p r0, #3\n    beq done\n    b main\ndone:\n   import bx lr\n",
  "{% extends \"base.htmlblock content %}\n  <h1>{{ title|upper }}</h1>\n  {% for item in items %}\n    <li>{{ item.name|default(\"none\") }}</li>\n  {% endfor %}\n{% endblock let%}\n",
  "const int LED_PIN = 13;\n\nvoid setup() {\n    pinMode(LED_PIN, OTPUT);%\n    Seri9600);\n}\n\nvoid loop() {\n    digitalWrite(LED_PIN, HIGH);\n}\n",
];
for (const code of autoCases) {
  const result = hljs.highlightAuto(code);
  cases.push({
    name: `auto-${cases.length}`,
    auto: true,
    code,
    language: result.language,
    relevance: result.relevance,
    html: result.value,
    second: result.secondBest ? result.secondBest.language : null,
    secondRelevance: result.secondBest ? result.secondBest.relevance : null,
  });
}
process.stdout.write(JSON.stringify(cases, null, 1) + '\n');
