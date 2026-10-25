// Reference: pi v1.4.0 (4a080252), core/bash-executor.ts.
// Regenerate from repository root:
// node coding/testdata/bash-executor/generate.mjs ./pi > coding/testdata/bash-executor/golden.json
// Execute the pinned function with its real helpers. The output-stream wrapper
// only observes finish, so fixture reads never race the reference's async flush.
import { readFileSync, mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { pathToFileURL } from "node:url";
import { stripTypeScriptTypes } from "node:module";
import { finished } from "node:stream/promises";
import { createHash } from "node:crypto";

const root = resolve(process.argv[2] ?? "./pi");
const load = (path) => import(pathToFileURL(join(root, path)).href);
const { createOutputFileStream } = await load("packages/coding-agent/src/utils/output-files.ts");
const { splitIncompleteAnsiSuffix, stripAnsi } = await load("packages/coding-agent/src/utils/ansi.ts");
const { sanitizeBinaryOutput } = await load("packages/coding-agent/src/utils/shell.ts");
const { DEFAULT_MAX_BYTES, truncateTail } = await load("packages/coding-agent/src/core/tools/truncate.ts");
const source = readFileSync(join(root, "packages/coding-agent/src/core/bash-executor.ts"), "utf8");
const start = source.indexOf("export async function executeBashWithOperations(");
if (start < 0) throw new Error("reference function marker changed");
const body = stripTypeScriptTypes(source.slice(start).replace("export async function", "async function"));
const instantiate = new Function("createOutputFileStream", "splitIncompleteAnsiSuffix", "stripAnsi", "sanitizeBinaryOutput", "DEFAULT_MAX_BYTES", "truncateTail", `${body}\nreturn executeBashWithOperations;`);
const cases = [
	{ name: "small", chunks: [{ text: "hello\n" }, { text: "tail" }] },
	{ name: "ascii-burst", chunks: [{ text: "x", repeat: 40000 }, { text: "tail" }] },
	{ name: "bmp-burst", chunks: [{ text: "漢", repeat: 40000 }, { text: "tail" }] },
	{ name: "astral-burst", chunks: [{ text: "😀", repeat: 30000 }, { text: "tail" }] },
	{ name: "ascii-truncated", chunks: [{ text: "x", repeat: 60000 }, { text: "tail" }] },
	{ name: "bmp-small-chunks", chunks: [...Array.from({ length: 4 }, () => ({ text: "漢", repeat: 10000 })), { text: "tail" }] },
];
const sha256 = (value) => createHash("sha256").update(value).digest("hex");
const directory = mkdtempSync(join(tmpdir(), "pier-bash-executor-"));
const previousTmp = process.env.TMPDIR;
process.env.TMPDIR = directory;
const output = [];
try {
	for (const test of cases) {
		const streams = [];
		const execute = instantiate((...args) => {
			const opened = createOutputFileStream(...args);
			streams.push(opened.stream);
			return opened;
		}, splitIncompleteAnsiSuffix, stripAnsi, sanitizeBinaryOutput, DEFAULT_MAX_BYTES, truncateTail);
		const result = await execute("fixture", directory, {
			exec: async (_command, _cwd, options) => {
				for (const chunk of test.chunks) options.onData(Buffer.from(chunk.text.repeat(chunk.repeat ?? 1)));
				return { exitCode: 0 };
			},
		});
		await Promise.all(streams.map((stream) => finished(stream)));
		const expected = {
			outputSHA256: sha256(result.output), outputBytes: Buffer.byteLength(result.output),
			truncated: result.truncated, cancelled: result.cancelled, exitCode: result.exitCode,
			hasFile: !!result.fullOutputPath,
		};
		if (result.fullOutputPath) expected.fileSHA256 = sha256(readFileSync(result.fullOutputPath));
		output.push({ ...test, expected });
	}
} finally {
	if (previousTmp === undefined) delete process.env.TMPDIR;
	else process.env.TMPDIR = previousTmp;
	rmSync(directory, { recursive: true, force: true });
}
console.log(JSON.stringify(output, null, 2));
