// Regenerates tui/testdata/md_golden.txt from the pinned reference's markdown
// pipeline: pi v1.5.2 (b4afdddf) tui/src/components/markdown.ts (marked 18.0.11
// with the marked-cjk-friendly emStrong tokenizer) lexed over md_corpus.json.
// The projection drops marked's bookkeeping fields and keeps the ones the Go
// test's projectBlock/projectInline read, in canonical (sorted-key) JSON.
//
//	node --experimental-strip-types tui/testdata/md/generate.mjs ./pi
//
// It rewrites the golden in place; `just goldens` runs it and requires the tree
// to stay clean.
import { readFileSync, writeFileSync } from "node:fs";
import { registerHooks } from "node:module";
import { dirname, resolve } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const root = resolve(process.argv[2] ?? "./pi");

// The reference's markdown pipeline (parser options, the CJK emStrong tokenizer,
// the LaTeX extensions and trimPartialClosingFences) is module state, not
// exports. Append the exports in memory rather than duplicating that logic here.
const markdownURL = pathToFileURL(resolve(root, "packages/tui/src/components/markdown.ts")).href;
registerHooks({
	load(url, context, next) {
		const result = next(url, context);
		if (url !== markdownURL) return result;
		return {
			...result,
			source: String(result.source) + "\nexport { markdownParser, trimPartialClosingFences };\n",
		};
	},
});
const { markdownParser, trimPartialClosingFences } = await import(markdownURL);

const BLOCK_FIELDS = {
	paragraph: ["text", "inlines"],
	heading: ["depth", "text", "inlines"],
	code: ["lang", "raw", "text"],
	hr: [],
	blockquote: ["text", "blocks"],
	list: ["items", "ordered", "start"],
	list_item: ["task", "checked", "text", "blocks"],
	table: ["align", "header", "rows"],
	html: ["text"],
	space: [],
	latexBlock: ["raw", "text", "pending"],
	checkbox: [],
	text: ["text", "inlines"],
};
const INLINE_FIELDS = {
	text: ["text"],
	codespan: ["text"],
	html: ["text"],
	strong: ["text", "tokens"],
	em: ["text", "tokens"],
	del: ["text", "tokens"],
	link: ["href", "title", "text", "tokens"],
	image: ["href", "title", "text", "tokens"],
	br: [],
	escape: ["raw", "text"],
	latex: ["raw", "text", "pending"],
	checkbox: [],
};

function projectInline(token) {
	const fields = INLINE_FIELDS[token.type];
	if (!fields) throw new Error(`unmapped inline token ${token.type}`);
	const out = { type: token.type };
	for (const field of fields) {
		switch (field) {
			case "tokens":
				out.tokens = (token.tokens ?? []).map(projectInline);
				break;
			case "text":
				out.text = token.text ?? "";
				break;
			case "href":
				out.href = token.href ?? "";
				break;
			case "title":
				if (token.title) out.title = token.title;
				break;
			case "raw":
				out.raw = token.raw ?? "";
				break;
			case "pending":
				if (token.pending) out.pending = true;
				break;
			default:
				out[field] = token[field];
		}
	}
	return out;
}

function projectCell(cell, align) {
	const out = { text: cell.text ?? "", tokens: (cell.tokens ?? []).map(projectInline) };
	if (align) out.align = align;
	return out;
}

function projectBlock(token) {
	const fields = BLOCK_FIELDS[token.type];
	if (!fields) throw new Error(`unmapped block token ${token.type}`);
	const out = { type: token.type };
	for (const field of fields) {
		switch (field) {
			case "inlines":
				out.tokens = (token.tokens ?? []).map(projectInline);
				break;
			case "blocks":
				out.tokens = (token.tokens ?? []).map(projectBlock);
				break;
			case "items":
				out.items = (token.items ?? []).map(projectBlock);
				break;
			case "header":
				out.header = (token.header ?? []).map((cell, index) => projectCell(cell, token.align?.[index]));
				break;
			case "rows":
				if (token.rows?.length) {
					out.rows = token.rows.map((row) => row.map((cell, index) => projectCell(cell, token.align?.[index])));
				}
				break;
			case "align":
				out.align = (token.align ?? []).map((value) => value ?? null);
				break;
			case "text":
				// A code block omits an empty body, as the projection does.
				if (token.type !== "code" || token.text) out.text = token.text ?? "";
				break;
			case "lang":
				if (token.lang) out.lang = token.lang;
				break;
			case "raw":
				if (token.raw) out.raw = token.raw;
				break;
			case "ordered":
				if (token.ordered) out.ordered = true;
				break;
			case "start":
				if (token.ordered) out.start = token.start;
				break;
			case "task":
				if (token.task) out.task = true;
				break;
			case "checked":
				if (token.task) out.checked = Boolean(token.checked);
				break;
			case "pending":
				if (token.pending) out.pending = true;
				break;
			default:
				if (token[field] !== undefined) out[field] = token[field];
		}
	}
	return out;
}

// Canonical JSON: sorted keys, compact, matching the Go test's stableValue.
function canonical(value) {
	if (Array.isArray(value)) return "[" + value.map(canonical).join(",") + "]";
	if (value && typeof value === "object") {
		const keys = Object.keys(value).sort();
		return "{" + keys.map((key) => JSON.stringify(key) + ":" + canonical(value[key])).join(",") + "}";
	}
	return JSON.stringify(value);
}

const corpus = JSON.parse(readFileSync(resolve(here, "../md_corpus.json"), "utf8"));
const lines = [];
for (const label of Object.keys(corpus)) {
	const tokens = markdownParser.lexer(corpus[label]);
	trimPartialClosingFences(tokens);
	lines.push(`${label} => ${canonical(tokens.map(projectBlock))}`);
}
writeFileSync(resolve(here, "../md_golden.txt"), lines.join("\n") + "\n");
console.log(`md golden: ${lines.length} cases`);
