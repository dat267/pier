// Reference: pi v1.5.2 (b4afdddf) tui/src/components/markdown.ts, the Markdown
// component constructed exactly as tui/markdown_test.go constructs the port
// (no padding, no default style, default options) and rendered with the same
// tagging theme. The documents are a seeded mix of markdown constructs, so the
// port's renderer is compared against the reference on shapes the curated corpus
// does not spell out.
//
// Regenerate from the repository root:
// node --experimental-strip-types tui/testdata/mdrender-fuzz/generate.mjs ./pi > tui/testdata/mdrender-fuzz/golden.json
import { resolve } from "node:path";
import { pathToFileURL } from "node:url";

const root = resolve(process.argv[2] ?? "./pi");
const { Markdown } = await import(pathToFileURL(resolve(root, "packages/tui/src/components/markdown.ts")).href);

const tag = (name) => (text) => `<${name}>${text}</${name}>`;
const theme = {
	heading: tag("h"),
	link: tag("link"),
	linkUrl: tag("url"),
	code: tag("code"),
	codeBlock: tag("cb"),
	codeBlockBorder: tag("cbb"),
	quote: tag("q"),
	quoteBorder: tag("qb"),
	hr: tag("hr"),
	listBullet: tag("lb"),
	bold: tag("b"),
	italic: tag("i"),
	strikethrough: tag("s"),
	underline: tag("u"),
};

let seed = Number(process.env.MD_FUZZ_SEED ?? "987654321");
const rand = () => {
	seed = (Math.imul(seed, 1103515245) + 12345) & 0x7fffffff;
	return seed / 0x80000000;
};
const pick = (list) => list[Math.floor(rand() * list.length) % list.length];

const words = ["alpha", "beta", "gamma", "delta", "x", "a-very-long-word-that-wraps", "漢字", "😀", "café", "under_score", "1.5", "<tag>", "a&b"];
const inline = [
	"**bold**",
	"*italic*",
	"_under_",
	"~~strike~~",
	"`code`",
	"``a ` b``",
	"[label](http://example.com/x)",
	"<http://auto.example>",
	"![alt](pic.png)",
	"\\*escaped\\*",
	"$x^2$",
	"<b>html</b>",
	"text with  \nsoft break",
];
const paragraph = () => {
	const parts = [];
	const count = 1 + Math.floor(rand() * 6);
	for (let index = 0; index < count; index++) parts.push(rand() < 0.35 ? pick(inline) : pick(words));
	return parts.join(" ");
};
const listBlock = () => {
	const marker = pick(["-", "*", "+", "1.", "2)", "5."]);
	const lines = [];
	const count = 1 + Math.floor(rand() * 3);
	for (let index = 0; index < count; index++) {
		const indent = rand() < 0.3 ? "  " : "";
		const task = rand() < 0.2 ? pick(["[ ] ", "[x] "]) : "";
		lines.push(`${indent}${marker} ${task}${paragraph()}`);
	}
	return lines.join("\n");
};
const quoteBlock = () => {
	const depth = rand() < 0.3 ? "> > " : "> ";
	const count = 1 + Math.floor(rand() * 2);
	const lines = [];
	for (let index = 0; index < count; index++) lines.push(`${depth}${paragraph()}`);
	return lines.join("\n");
};
const fenceBlock = () => {
	const fence = rand() < 0.3 ? "~~~" : "```";
	const language = pick(["", "js", "python", "go"]);
	const count = 1 + Math.floor(rand() * 3);
	const lines = [];
	for (let index = 0; index < count; index++) lines.push(`${index === 0 && rand() < 0.3 ? "  " : ""}${paragraph()}`);
	return `${fence}${language}\n${lines.join("\n")}\n${fence}`;
};
const tableBlock = () => {
	const columns = 1 + Math.floor(rand() * 3);
	const cell = () => pick(["a", "**b**", "`c`", "long cell value here", "漢字", "1"]);
	const header = Array.from({ length: columns }, cell).join(" | ");
	const align = Array.from({ length: columns }, () => pick(["-", ":-", "-:", ":-:"])).join(" | ");
	const row = Array.from({ length: columns }, cell).join(" | ");
	const rows = rand() < 0.5 ? [row, row] : [row];
	return `| ${header} |\n| ${align} |\n${rows.map((value) => `| ${value} |`).join("\n")}`;
};
const block = () => {
	const kind = rand();
	if (kind < 0.28) return paragraph();
	if (kind < 0.46) return listBlock();
	if (kind < 0.58) return quoteBlock();
	if (kind < 0.7) return fenceBlock();
	if (kind < 0.82) return tableBlock();
	if (kind < 0.88) return `${pick(["#", "##", "###", "####"])} ${paragraph()}`;
	if (kind < 0.94) return pick(["---", "***", "___"]);
	return `    ${paragraph()}`;
};

const documents = [];
for (let index = 0; index < 150; index++) {
	const count = 1 + Math.floor(rand() * 4);
	const blocks = [];
	for (let part = 0; part < count; part++) blocks.push(block());
	const separator = rand() < 0.7 ? "\n\n" : "\n";
	documents.push(blocks.join(separator));
}

const widths = [8, 12, 20, 33, 40];
const fixtures = [];
for (const [index, text] of documents.entries()) {
	const width = widths[Math.floor(rand() * widths.length) % widths.length];
	const component = new Markdown(text, 0, 0, theme, undefined, {});
	fixtures.push({ label: `fuzz-${index}`, text, width, lines: component.render(width) });
}
console.log(JSON.stringify(fixtures, null, 1));
