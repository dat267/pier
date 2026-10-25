// Regenerate from the repository root:
// node coding/testdata/mcp-tools/generate.mjs ./pi > coding/testdata/mcp-tools/golden.json
// Reference: pi v1.4.0 (4a080252), packages/coding-agent/src/extensions/mcp/tools.ts.
// Executes the pinned createMcpToolName and toModelContent, never an installed bundle.
import { resolve, join } from "node:path";
import { pathToFileURL } from "node:url";

const root = resolve(process.argv[2] ?? "./pi");
const { createMcpToolName, toModelContent } = await import(
	pathToFileURL(join(root, "packages/coding-agent/src/extensions/mcp/tools.ts")).href,
);

const nameCases = [
	["plain", "srv", "tool", false],
	["hyphen", "srv", "tool-name", false],
	["dot-space-slash", "a+b", "c d/e.f", false],
	["bmp-accent", "srv", "outil-é", false],
	["astral-tool", "srv", "😀", false],
	["astral-inside", "srv", "a😀b", false],
	["astral-many", "srv", "😀".repeat(20), false],
	["astral-server", "server😀", "tool", false],
	["astral-taken", "srv", "😀", true],
	["astral-long", "srv", "😀".repeat(40), true],
	["ascii-64", "srv", "x".repeat(64), false],
	["ascii-65", "srv", "x".repeat(65), false],
	["ascii-long", "srv", "x".repeat(200), false],
	["taken-short", "srv", "tool", true],
];
const toolNames = nameCases.map(([name, server, tool, taken]) => ({
	name,
	server,
	tool,
	taken,
	expected: createMcpToolName(server, tool, () => taken),
}));

const link = (extra) => [{ type: "resource_link", uri: "file:///a.txt", name: "a", ...extra }];
const blob = (uri, value, mimeType) => [{ type: "resource", resource: { uri, blob: value, mimeType } }];
const contentCases = {
	"link-minimal": link({}),
	"link-full": link({ title: "A", mimeType: "text/plain", size: 1536, description: "the a file" }),
	"link-empty-description": link({ description: "" }),
	"link-null-description": link({ description: null }),
	"link-null-title": link({ title: null }),
	"link-size-zero": link({ size: 0 }),
	"resource-text-arm": [{ type: "resource", resource: { uri: "file:///b.txt", text: "inline body", mimeType: "text/plain" } }],
	"resource-empty-text-arm": [{ type: "resource", resource: { uri: "file:///b.txt", text: "", mimeType: "text/plain" } }],
	"resource-neither-arm": [{ type: "resource", resource: { uri: "file:///h.txt" } }],
	"resource-empty-blob": blob("file:///e.bin", "", "application/octet-stream"),
	"resource-both-arms": [{ type: "resource", resource: { uri: "file:///both.bin", text: "inline", blob: "AAEC", mimeType: "application/octet-stream" } }],
	"resource-text-blob": blob("file:///c.txt", "aGVsbG8=", "text/plain"),
	"resource-json-blob": blob("file:///d.json", "eyJuIjoxfQ==", "application/json; charset=utf-8"),
	"resource-binary-blob": blob("file:///e.pdf", "AAEC", "application/pdf"),
	"resource-image-blob": blob("file:///g.png", "aW1n", "image/png"),
	"resource-wrapped-blob": blob("file:///i.bin", "aGVs\nbG8g\nd29y\nbGQ=", "application/octet-stream"),
	"base64url-one": blob("file:///u1.bin", "_-", "application/octet-stream"),
	"base64url-two": blob("file:///u2.bin", "a-b", "application/octet-stream"),
	"base64url-three": blob("file:///u3.bin", "AA_A", "application/octet-stream"),
	"base64url-text": blob("file:///u4.txt", "aGVsbG8_", "text/plain"),
	"uri-opaque-mailto": blob("mailto:a@b.txt", "AAEC", "application/octet-stream"),
	"uri-opaque-urn": blob("urn:a.b", "AAEC", "application/octet-stream"),
	"uri-drive-letter": blob("C:\\dir\\file.txt", "AAEC", "application/octet-stream"),
	"uri-file-query": blob("file:///y.tar.gz?v=1", "AAEC", "application/octet-stream"),
	"uri-raw-query": blob("x.txt?a.b", "AAEC", "application/octet-stream"),
	"uri-no-extension": blob("file:///noext", "AAEC", "application/octet-stream"),
	"save-failure": blob("file:///j.fail", "AAEC", "application/octet-stream"),
	"text-block": [{ type: "text", text: "hello" }],
	"image-block": [{ type: "image", data: "aW1n", mimeType: "image/png" }],
	"audio-block": [{ type: "audio", data: "YXVk", mimeType: "audio/wav" }],
};
const results = {};
for (const [name, blocks] of Object.entries(contentCases)) {
	const saveCalls = [];
	const saveOutput = async (data, extension) => {
		const bytes = typeof data === "string" ? Buffer.byteLength(data) : data.length;
		saveCalls.push({ bytes, extension });
		if (extension === ".fail") throw new Error("disk full");
		return "/tmp/full" + extension;
	};
	try {
		const content = await toModelContent("srv", blocks, { saveOutput, readableResources: true });
		results[name] = {
			blocks,
			content: content.map((block) =>
				block.type === "text"
					? { type: "text", text: block.text }
					: { type: "image", mimeType: block.mimeType, data: block.data },
			),
			saveCalls,
		};
	} catch (error) {
		results[name] = { blocks, error: error.constructor.name };
	}
}
console.log(JSON.stringify({ toolNames, content: results }, null, 2));
