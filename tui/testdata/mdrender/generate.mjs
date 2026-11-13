// Regenerates tui/testdata/md_render_golden.txt from the pinned reference's
// Markdown component: pi v1.5.2 (b4afdddf) tui/src/components/markdown.ts,
// constructed exactly as tui/markdown_test.go constructs the port (padding,
// default style, options) and rendered with the tagging theme that test uses, so
// each line records which theme function styled which run of text.
//
//	node --experimental-strip-types tui/testdata/mdrender/generate.mjs ./pi
//
// It rewrites the golden in place; `just goldens` runs it and requires the tree
// to stay clean.
import { writeFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const root = resolve(process.argv[2] ?? "./pi");
const markdownURL = pathToFileURL(resolve(root, "packages/tui/src/components/markdown.ts")).href;
const { Markdown } = await import(markdownURL);

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

const upper = (markdown) => markdown.toUpperCase();

// The cases mirror tui/markdown_test.go: text, widths, padding, default style
// and options, in the order the golden records them.
const cases = [
	{ label: "para", text: "Hello world", widths: [20, 5] },
	{ label: "para-spacing", text: "one\n\ntwo", widths: [20] },
	{ label: "headings", text: "# H1\n\n## H2\n\n### H3\n\n#### H4", widths: [24] },
	{ label: "headings-inline", text: "# A **b** and `c`", widths: [30] },
	{ label: "emphasis", text: "a *em* and **strong** and ~~del~~ and `code`", widths: [40] },
	{ label: "fence", text: "```js\nconst x = 1;\n```", widths: [30] },
	{ label: "fence-long", text: "```python\ndef f(x):\n    return x + 1\n```", widths: [20] },
	{ label: "indented-code", text: "    indented code", widths: [20] },
	{ label: "ul", text: "- a\n- b\n- c", widths: [20] },
	{ label: "ul-nested", text: "- a\n  - b\n    - c\n- d", widths: [24] },
	{ label: "ol", text: "1. one\n2. two", widths: [20] },
	{ label: "ol-start", text: "5. five\n6. six", widths: [20] },
	{ label: "task", text: "- [ ] todo\n- [x] done", widths: [24] },
	{ label: "loose", text: "- a\n\n- b", widths: [20] },
	{ label: "quote", text: "> quoted text here", widths: [20] },
	{ label: "quote-multi", text: "> para\n>\n> - a\n> - b", widths: [24] },
	{ label: "quote-nested", text: "> > deep", widths: [20] },
	{ label: "quote-spacing", text: "before\n\n> quote\n\nafter", widths: [24] },
	{ label: "hr", text: "a\n\n---\n\nb", widths: [10] },
	{ label: "table", text: "| a | b |\n| - | - |\n| 1 | 2 |\n| 3 | 4 |", widths: [30, 20] },
	{ label: "table-align", text: "| left | center | right |\n| :- | :-: | -: |\n| a | b | c |", widths: [40] },
	{ label: "table-wrap", text: "| col |\n| - |\n| a very long cell value here |", widths: [20] },
	{ label: "table-narrow", text: "| a | b | c |\n| - | - | - |\n| 1 | 2 | 3 |", widths: [8] },
	{ label: "table-inline", text: "| **b** | `c` |\n| - | - |\n| [l](u) | x |", widths: [30] },
	{ label: "links", text: "[text](http://x) and [http://y](http://y) and <http://z>", widths: [40] },
	{ label: "image", text: "![alt](img.png)", widths: [20] },
	{ label: "html", text: "<div>\nraw\n</div>", widths: [20] },
	{ label: "inline-html", text: "text <b>bold</b> text", widths: [30] },
	{ label: "escapes", text: "a \\* b \\_ c", widths: [20] },
	{ label: "br", text: "line one  \nline two", widths: [20] },
	{ label: "mixed", text: "# Title\n\npara **b**\n\n- item one\n- item two\n\n> quote\n\n```py\nx=1\n```\n\n| a |\n| - |\n| 1 |", widths: [30] },
	{ label: "padding", text: "text", widths: [20], paddingX: 2, paddingY: 1 },
	{ label: "default-style", text: "plain **bold** text", widths: [24], defaultStyle: { bold: true } },
	{ label: "transform", text: "abc", widths: [20] },
	{ label: "preserve-markers", text: "- a\n1. b", widths: [20], options: { preserveOrderedListMarkers: true } },
	{ label: "preserve-escapes", text: "a \\* b", widths: [20], options: { preserveBackslashEscapes: true } },
	{ label: "no-latex", text: "math $x$ here", widths: [20], options: { renderLatex: false } },
	// Appended after the first generation, so they stay at the end of the file.
	{ label: "default-style-full", text: "plain **bold** text", widths: [24], defaultStyle: { color: tag("col"), bgColor: tag("bg"), bold: true, italic: true } },
	{ label: "transform-upper", text: "abc", widths: [20], options: { transform: upper } },
	{ label: "latex-fallback", text: "math $x^2$ here", widths: [20] },
];

const lines = [];
for (const tc of cases) {
	for (const width of tc.widths) {
		const component = new Markdown(tc.text, tc.paddingX ?? 0, tc.paddingY ?? 0, theme, tc.defaultStyle, tc.options ?? {});
		lines.push(`md ${tc.label}@${width} => ${JSON.stringify(component.render(width))}`);
	}
}
writeFileSync(resolve(here, "../md_render_golden.txt"), lines.join("\n") + "\n");
console.log(`md render golden: ${lines.length} lines`);
