// Reference: pi v1.4.0 (4a080252), core/tools/output-accumulator.ts.
// Regenerate from repository root:
// node coding/testdata/output-accumulator/generate.mjs ./pi > coding/testdata/output-accumulator/golden.json
// Execute the pinned class with its real helpers. The output-stream factory is
// wrapped only to await each stream's close before reading, so fixture reads
// never race the reference's asynchronous flush.
import { readFileSync, mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { pathToFileURL } from "node:url";
import { finished } from "node:stream/promises";
import { createHash } from "node:crypto";
import { stripTypeScriptTypes } from "node:module";

const root = resolve(process.argv[2] ?? "./pi");
const load = (path) => import(pathToFileURL(join(root, path)).href);
const { createOutputFileStream } = await load("packages/coding-agent/src/utils/output-files.ts");
const { DEFAULT_MAX_BYTES, DEFAULT_MAX_LINES, truncateTail } = await load(
	"packages/coding-agent/src/core/tools/truncate.ts",
);

const source = readFileSync(join(root, "packages/coding-agent/src/core/tools/output-accumulator.ts"), "utf8");
const start = source.indexOf("export class OutputAccumulator");
if (start < 0) throw new Error("reference class marker changed");
const body = stripTypeScriptTypes(source.slice(start).replace("export class", "class"));

// byteLength is a module-level helper in the reference file; pass its source too.
const helperStart = source.indexOf("function byteLength(");
const helperEnd = source.indexOf("\n}", helperStart) + 2;
if (helperStart < 0) throw new Error("reference byteLength helper marker changed");
const byteLengthSource = stripTypeScriptTypes(source.slice(helperStart, helperEnd));

const build = (factory) =>
	new Function(
		"createOutputFileStream",
		"DEFAULT_MAX_BYTES",
		"DEFAULT_MAX_LINES",
		"truncateTail",
		`${byteLengthSource}\n${body}\nreturn OutputAccumulator;`,
	)(factory, DEFAULT_MAX_BYTES, DEFAULT_MAX_LINES, truncateTail);

const repeatLines = (prefix, count) => Array.from({ length: count }, (_, index) => `${prefix}${index}`).join("\n");
const cases = [
	{ name: "empty", chunks: [""] },
	{ name: "no-trailing-newline", chunks: ["first\nsecond"] },
	{ name: "trailing-newline", chunks: ["first\nsecond\n"] },
	{ name: "many-lines", chunks: [`${repeatLines("row", DEFAULT_MAX_LINES + 20)}\n`] },
	{ name: "long-line", chunks: ["x".repeat(DEFAULT_MAX_BYTES + 100)] },
	{ name: "multibyte-long-line", chunks: ["漢".repeat(Math.floor(DEFAULT_MAX_BYTES / 3) + 100)] },
	{ name: "crlf", chunks: ["a\r\nb\r\nc\r\n"] },
	{ name: "bom", chunks: ["\ufeffhello\nworld"] },
	{ name: "split-chunks", chunks: ["alpha\n", "beta", "\ngamma\n", "delta"] },
	{ name: "split-multibyte", chunks: ["漢字\n", Buffer.from([0xe6, 0xbc]), Buffer.from([0xa2, 0x0a, 0x78])] },
	{ name: "chunked-large", chunks: Array.from({ length: 12 }, (_, index) => `${repeatLines(`block${index}-`, 200)}\n`) },
	{ name: "byte-and-line-limits", options: { maxLines: 3, maxBytes: 40 }, chunks: ["one\ntwo\nthree\nfour\nfive\n"] },
];
const sha256 = (value) => createHash("sha256").update(value).digest("hex");

const directory = mkdtempSync(join(tmpdir(), "pier-output-accumulator-"));
const previousTmp = process.env.TMPDIR;
process.env.TMPDIR = directory;
const output = [];
try {
	for (const test of cases) {
		const streams = [];
		const Accumulator = build((...args) => {
			const opened = createOutputFileStream(...args);
			streams.push(opened.stream);
			return opened;
		});
		const accumulator = new Accumulator(test.options ?? {});
		for (const chunk of test.chunks) {
			accumulator.append(Buffer.isBuffer(chunk) ? chunk : Buffer.from(chunk));
		}
		accumulator.finish();
		const snapshot = accumulator.snapshot({ persistIfTruncated: true });
		await accumulator.closeTempFile();
		await Promise.all(streams.map((stream) => finished(stream)));
		const expected = {
			contentSHA256: sha256(snapshot.content),
			contentBytes: Buffer.byteLength(snapshot.content),
			truncated: snapshot.truncation.truncated,
			truncatedBy: snapshot.truncation.truncatedBy ?? "",
			totalLines: snapshot.truncation.totalLines,
			totalBytes: snapshot.truncation.totalBytes,
			outputLines: snapshot.truncation.outputLines,
			outputBytes: snapshot.truncation.outputBytes,
			lastLinePartial: snapshot.truncation.lastLinePartial ?? false,
			fullOutputPath: snapshot.fullOutputPath ? "present" : "",
		};
		if (snapshot.fullOutputPath) expected.fileSHA256 = sha256(readFileSync(snapshot.fullOutputPath));
		output.push({ ...test, expected });
	}
} finally {
	if (previousTmp === undefined) delete process.env.TMPDIR;
	else process.env.TMPDIR = previousTmp;
	rmSync(directory, { recursive: true, force: true });
}
console.log(JSON.stringify(output, null, 2));
