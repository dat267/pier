// Generates upstream goldens for the ported terminal-image pure functions.
// Run from the repo root:
//   node --experimental-strip-types tui/testdata/gen_terminal_image.mjs > tui/testdata/terminal_image_golden.txt
import { Buffer } from "node:buffer";

const moduleURL = new URL("../../pi/packages/tui/src/terminal-image.ts", import.meta.url);
const {
	encodeKitty,
	encodeITerm2,
	deleteKittyImage,
	deleteAllKittyImages,
	deleteAllKittyPlacements,
	calculateImageCellSize,
	calculateImageRows,
	getPngDimensions,
	getJpegDimensions,
	getGifDimensions,
	getWebpDimensions,
	isImageLine,
} = await import(moduleURL.href);

const lines = [];
const emit = (label, value) => lines.push(`${label} => ${JSON.stringify(value)}`);

// ---- Kitty encoding ----
emit("kitty.small", encodeKitty("AAAA"));
emit("kitty.columns-rows", encodeKitty("AAAA", { columns: 2, rows: 3 }));
emit("kitty.image-id", encodeKitty("AAAA", { imageId: 42 }));
emit("kitty.no-move", encodeKitty("AAAA", { columns: 2, rows: 2, moveCursor: false }));
emit("kitty.chunked", encodeKitty("A".repeat(10000), { columns: 5, imageId: 7, moveCursor: false }));

// ---- iTerm2 encoding ----
emit("iterm2.basic", encodeITerm2("AAAA"));
emit("iterm2.width-height", encodeITerm2("AAAA", { width: 2, height: "auto" }));
emit("iterm2.name", encodeITerm2("AAAA", { name: "pixel.png" }));
emit("iterm2.no-aspect", encodeITerm2("AAAA", { width: "10", preserveAspectRatio: false }));
emit("iterm2.not-inline", encodeITerm2("AAAA", { inline: false }));

// ---- Deletes ----
emit("delete.image", deleteKittyImage(42));
emit("delete.all-images", deleteAllKittyImages());
emit("delete.all-placements", deleteAllKittyPlacements());

// ---- Cell sizing grid ----
const cellSizes = [
	{ widthPx: 9, heightPx: 18 },
	{ widthPx: 10, heightPx: 10 },
	{ widthPx: 14, heightPx: 28 },
	{ widthPx: 1, heightPx: 1 },
];
const images = [
	{ widthPx: 615, heightPx: 86 },
	{ widthPx: 1200, heightPx: 12 },
	{ widthPx: 400, heightPx: 900 },
	{ widthPx: 149, heightPx: 1000 },
	{ widthPx: 1, heightPx: 1000 },
];
const maxWidths = [2, 30, 60];
const maxHeights = [undefined, 5, 10, 15, 30];
for (const cells of cellSizes) {
	for (const image of images) {
		for (const maxWidth of maxWidths) {
			for (const maxHeight of maxHeights) {
				for (const optimize of [false, true]) {
					const size = calculateImageCellSize(image, maxWidth, maxHeight, cells, optimize);
					emit(
						`cells|${image.widthPx}x${image.heightPx}|${maxWidth}|${maxHeight ?? -1}|${cells.widthPx}x${cells.heightPx}|${optimize}`,
						{ columns: size.columns, rows: size.rows },
					);
					emit(
						`rows|${image.widthPx}x${image.heightPx}|${maxWidth}|${cells.widthPx}x${cells.heightPx}`,
						calculateImageRows(image, maxWidth, cells),
					);
				}
			}
		}
	}
}

// ---- Dimension parsers ----
const fixtures = {
	"png.40x10": Buffer.from("89504e470d0a1a0a0000000d49484452000000280000000a", "hex"),
	"jpeg.8x6": Buffer.from("ffd8ffe000104a46494600010100000100010000ffc00011080006000803012200021101031101ffd9", "hex"),
	"gif.32x16": Buffer.from("47494638396120001000", "hex"),
	"webp.2x3": Buffer.from("52494646b000000057454250565038580a00000001000000000000000200000003", "hex"),
};
for (const [label, buffer] of Object.entries(fixtures)) {
	const base64 = buffer.toString("base64");
	emit(`png|${label}`, getPngDimensions(base64));
	emit(`jpeg|${label}`, getJpegDimensions(base64));
	emit(`gif|${label}`, getGifDimensions(base64));
	emit(`webp|${label}`, getWebpDimensions(base64));
}

// ---- isImageLine ----
const imageLines = {
	"iterm2.start": "\x1b]1337;File=size=100,100;inline=1:base64encodeddata==\x07",
	"iterm2.middle": "Some text \x1b]1337;File=inline=1:data==\x07 more text",
	"kitty.start": "\x1b_Ga=T,f=100;data\x1b\\\x1b_Gm=i=1;\x1b\\",
	"kitty.middle": "Output: \x1b_Ga=T,f=100;data\x1b\\",
	"plain": "This is just a regular text line",
	"ansi-only": "\x1b[31mRed text\x1b[0m",
	"partial-iterm2": "Some text with ]1337;File but missing ESC",
	"partial-kitty": "Some text with _G but missing ESC",
	"empty": "",
};
for (const [label, value] of Object.entries(imageLines)) {
	emit(`isImageLine|${label}`, isImageLine(value));
}

process.stdout.write(lines.join("\n") + "\n");
