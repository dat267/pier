// pi v1.4.0 (4a080252), packages/tui/src/components/markdown.ts and its pinned
// marked 18.0.11 / marked-cjk-friendly 0.1.2 dependencies. Read-only source load.
// Install those versions in a temporary directory, then regenerate from root:
// NODE_PATH=<temporary directory>/node_modules node internal/markdown/testdata/angle-html/generate.mjs ./pi > internal/markdown/testdata/angle-html/fixtures.json
import { readFileSync } from "node:fs";
import { createRequire, stripTypeScriptTypes } from "node:module";
import { resolve } from "node:path";

const require = createRequire(import.meta.url);
for (const [name, version] of [["marked", "18.0.11"], ["marked-cjk-friendly", "0.1.2"]]) {
	if (require(`${name}/package.json`).version !== version) throw new Error(`Expected ${name} ${version}`);
}
const { Marked, Tokenizer } = require("marked");
const markedCjkFriendly = require("marked-cjk-friendly").default;
const root = resolve(process.argv[2] ?? "./pi");
const source = readFileSync(`${root}/packages/tui/src/components/markdown.ts`, "utf8");
const start = source.indexOf("const STRICT_STRIKETHROUGH_REGEX");
const end = source.indexOf("/**\n * Default text styling");
if (start < 0 || end <= start) throw new Error("Cannot locate reference Markdown configuration");
const setup = stripTypeScriptTypes(source.slice(start, end));
const parser = new Function("Marked", "Tokenizer", "markedCjkFriendly", `${setup}\nreturn markdownParser;`)(Marked, Tokenizer, markedCjkFriendly);

// Same token projection as internal/markdown/mdgolden_test.go. Ignore lexer
// bookkeeping and preserve all render-relevant fields, including child order.
function project(token) {
	const out = { type: token.type };
	if (["text", "codespan", "html"].includes(token.type)) out.text = token.text;
	if (["strong", "em", "del", "link", "image"].includes(token.type)) {
		out.text = token.text;
		out.tokens = token.tokens.map(project);
	}
	if (["link", "image"].includes(token.type)) {
		out.href = token.href;
		if (token.title) out.title = token.title;
	}
	if (["escape", "latex"].includes(token.type)) {
		out.raw = token.raw;
		out.text = token.text;
		if (token.pending) out.pending = true;
	}
	return out;
}

const cases = [];
function add(name, source) { cases.push({ name, source }); }
for (const source of [
	"<a:b>", "<a@b>", "<x:y z>", "<https://x.com >", "<see HTTP://Example.COM>",
	"<ab:c>", "<ab:>", "<a1:x>", "<a+.:x>", "<a-:x>", "<a.b:c>", "<1a:x>",
	"<https://a@b.com>", "<mailto:a@example.com>", "<http://x.com/例え>",
	`<${"a".repeat(32)}:x>`, `<${"a".repeat(33)}:x>`,
	"<a@example.com>", "<a+b_c-d.e@example.co.uk>", "<a!#$%&'*+/=?^_`{|}~-@b.c>",
	"<a@-b.com>", "<a@b-.com>", "<a@b_c.d>", "<a@b..c>", "<a@b.c->", "<a@b.c_>",
	`<a@${"b".repeat(63)}.com>`, `<a@${"b".repeat(64)}.com>`,
	`<a@b.${"c".repeat(63)}>`, `<a@b.${"c".repeat(64)}>`,
	"<b>", "</b>", "<br/>", "<br />", "<a:b>", "</a:b>", "<tag_name>", "<tag-name>",
	"<3tag>", "<_tag>", "<tag.name>", "<tag attr>", "<tag a:b=foo>", "<tag :x='v'>",
	"<tag _x=\"v\">", "<tag foo/bar>", "<tag a=b=c>", "<tag a=>", "<tag a=\"unclosed>",
	"<tag a='>'> tail", "<tag a=\"one>two\" b=three> tail", "<tag a='one\ntwo'>",
	"<tag a = b b = 'c'>", "</tag attr>", "</tag />", "</tag\n>",
	"<!-- comment -->", "<!-- a > b --> tail", "<!-->", "<!--->", "<!-- unclosed>",
	"<!-- one --> two -->", "<!-- -- -->", "<?pi x > y?> tail", "<?pi unclosed>",
	"<!DOCTYPE html>", "<!DOCTYPE>", "<!declaration x > tail", "<![CDATA[a > b]]> tail",
	"<![CDATA[unclosed>", "<<tag>>", "\\<tag>",
	"<a href='x'>https://y.com</a> https://z.com", "<A HREF='x'>www.y.com</A> www.z.com",
	"<a>https://y.com", "<a\thref='x'>https://y.com", // inLink requires the literal `<a `.
]) add(source, source);

const spaces = ["\t", "\n", "\v", "\f", "\r", " ", "\u00a0", "\u1680", "\u2000", "\u2003", "\u200a", "\u2028", "\u2029", "\u202f", "\u205f", "\u3000", "\ufeff"];
for (const space of spaces) {
	const label = JSON.stringify(space);
	add(`URI whitespace ${label}`, `<ab:x${space}y>`);
	add(`HTML attribute whitespace ${label}`, `<tag${space}attr${space}=${space}'v'>`);
	add(`HTML unquoted value whitespace ${label}`, `<tag attr=x${space}next>`);
	add(`HTML closing whitespace ${label}`, `</tag${space}>`);
	add(`declaration whitespace ${label}`, `<!DOCTYPE${space}html>`);
}
for (const control of ["\0", "\x01", "\x1f", "\x7f", "\u0085"]) {
	const label = JSON.stringify(control);
	add(`URI control ${label}`, `<ab:x${control}y>`);
	add(`HTML attribute control ${label}`, `<tag${control}attr='v'>`);
}
console.log(JSON.stringify(cases.map((item) => ({ ...item, tokens: parser.Lexer.lexInline(item.source, parser.defaults).map(project) })), null, 2));
