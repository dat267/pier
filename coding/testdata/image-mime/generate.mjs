// Reference: pi v1.4.0 (4a080252), utils/mime.ts detectSupportedImageMimeType.
// Regenerate from the repository root:
// node --experimental-strip-types coding/testdata/image-mime/generate.mjs ./pi > coding/testdata/image-mime/golden.json
import { registerHooks } from "node:module";
import { resolve } from "node:path";
import { pathToFileURL } from "node:url";
const root = resolve(process.argv[2] ?? "./pi");
registerHooks({ resolve(specifier, context, next) {
	const map = { "@earendil-works/pi-telemetry": "packages/telemetry/src/index.ts", "@earendil-works/pi-ai": "packages/ai/src/index.ts" };
	return map[specifier] ? { url: pathToFileURL(resolve(root, map[specifier])).href, shortCircuit: true } : next(specifier, context);
} });
const { detectSupportedImageMimeType } = await import(pathToFileURL(resolve(root, "packages/coding-agent/src/utils/mime.ts")));

const bytes = (...values) => Uint8Array.from(values);
const ascii = (text) => Uint8Array.from([...text].map((character) => character.charCodeAt(0)));
const be32 = (value) => bytes((value >>> 24) & 0xff, (value >>> 16) & 0xff, (value >>> 8) & 0xff, value & 0xff);
const le32 = (value) => bytes(value & 0xff, (value >>> 8) & 0xff, (value >>> 16) & 0xff, (value >>> 24) & 0xff);
const pngSignature = bytes(0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a);
const pngChunk = (type, data = []) => bytes(...be32(data.length), ...ascii(type), ...data, ...be32(0));
const validPng = Uint8Array.from([...pngSignature, ...pngChunk("IHDR", Array(13).fill(0)), ...pngChunk("IDAT", [1, 2, 3]), ...pngChunk("IEND")]);
const animatedPng = Uint8Array.from([...pngSignature, ...pngChunk("IHDR", Array(13).fill(0)), ...pngChunk("acTL", [0, 0, 0, 1, 0, 0, 0, 1]), ...pngChunk("IDAT", [1])]);
const pngWrongLength = Uint8Array.from([...pngSignature, ...bytes(0, 0, 0, 12), ...ascii("IHDR"), ...Array(12).fill(0)]);
const bmp = ({ declaredSize = 30, headerSize = 12, pixelOffset = 26, planes = 1, bpp = 24 } = {}) => {
	const size = headerSize >= 40 ? 34 : 30;
	const buffer = new Uint8Array(size);
	buffer.set(ascii("BM"), 0);
	buffer.set(le32(declaredSize), 2);
	buffer.set(le32(pixelOffset), 10);
	buffer.set(le32(headerSize), 14);
	const planesOffset = headerSize === 12 ? 22 : 26;
	buffer.set(bytes(planes & 0xff, (planes >>> 8) & 0xff), planesOffset);
	buffer.set(bytes(bpp & 0xff, (bpp >>> 8) & 0xff), planesOffset + 2);
	return buffer;
};
const cases = {
	"jpeg": bytes(0xff, 0xd8, 0xff, 0xe0, 0, 0),
	"jpeg2000": bytes(0xff, 0xd8, 0xff, 0xf7, 0, 0),
	"png-valid": validPng,
	"png-animated": animatedPng,
	"png-truncated-header": pngSignature,
	"png-wrong-ihdr-length": pngWrongLength,
	"png-no-ihdr-type": Uint8Array.from([...pngSignature, ...bytes(0, 0, 0, 13), ...ascii("XXXX"), ...Array(13).fill(0)]),
	"gif87a": ascii("GIF87a...."),
	"gif89a": ascii("GIF89a...."),
	"gif-wrong-version": ascii("GIF90a...."),
	"webp": Uint8Array.from([...ascii("RIFF"), ...le32(100), ...ascii("WEBPVP8 ")]),
	"webp-short": Uint8Array.from([...ascii("RIFF"), 0, 0, 0, 0]),
	"riff-not-webp": Uint8Array.from([...ascii("RIFF"), ...le32(100), ...ascii("WAVEfmt ")]),
	"bmp-valid-12": bmp(),
	"bmp-valid-40": bmp({ headerSize: 40, pixelOffset: 54, declaredSize: 56 }),
	"bmp-bad-planes": bmp({ planes: 2 }),
	"bmp-bad-bpp": bmp({ bpp: 3 }),
	"bmp-header-too-large": bmp({ headerSize: 200 }),
	"bmp-offset-before-header": bmp({ pixelOffset: 14 }),
	"bmp-declared-too-small": bmp({ declaredSize: 20 }),
	"bmp-declared-zero": bmp({ declaredSize: 0 }),
	"bmp-offset-at-declared": bmp({ headerSize: 40, pixelOffset: 56, declaredSize: 56 }),
	"bmp-size-mismatch": bmp({ declaredSize: 100 }),
	"bmp-too-short": Uint8Array.from([...ascii("BM"), ...le32(26), ...le32(12)]),
	"empty": new Uint8Array(0),
	"text": ascii("hello world"),
};
const fixtures = Object.entries(cases).map(([name, value]) => ({
	name,
	hex: Buffer.from(value).toString("hex"),
	expected: detectSupportedImageMimeType(value),
}));
console.log(JSON.stringify(fixtures, null, 2));
