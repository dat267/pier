// Reference: pi v1.5.2 (b4afdddf), packages/tui/src/colors.ts (colorToHex,
// colorToOklch, colorToOkhsl, foregroundAnsi, mixColors). A concrete color keeps
// fractional channels until it is written, so a mix reads the stored channels of
// an RGB color and the rounded channels of an OKLCH one; this corpus pins both
// rules plus the xterm-256 downgrade and the hex formatter.
//
// Regenerate from the repository root:
//   node --experimental-strip-types tui/testdata/colors/generate.mjs ./pi > tui/testdata/colors/golden.json
import { resolve } from "node:path";
import { pathToFileURL } from "node:url";

const root = resolve(process.argv[2] ?? "./pi");
const { oklchColor, rgbColor, indexedColor, colorToHex, colorToOklch, colorToOkhsl, foregroundAnsi, mixColors } =
	await import(pathToFileURL(resolve(root, "packages/tui/src/colors.ts")).href);

const spec = (color) => {
	if (color.kind === "indexed") return { type: "indexed", index: color.index };
	if (color.kind === "rgb") return { type: "rgb", r: color.r, g: color.g, b: color.b };
	return { type: "oklch", l: color.l, c: color.c, h: color.h };
};

const colors = [];
for (let li = 0; li <= 10; li += 2)
	for (let ci = 0; ci <= 8; ci += 2)
		for (let hi = 0; hi < 12; hi += 2) colors.push(oklchColor(li / 10, (ci / 10) * 0.5, hi * 30));
for (let index = 0; index < 256; index++) colors.push(indexedColor(index));
for (const channels of [
	[0, 0, 0],
	[255, 255, 255],
	[128, 0, 255],
	[47.5, 47.5, 47.5],
	[1, 2, 3],
	[254, 1, 1],
	[127.5, 63.75, 191.25],
	[127.5, 127.5, 127.5],
])
	colors.push(rgbColor(...channels));

const red = rgbColor(255, 0, 0);
const green = rgbColor(10, 200, 30);
const base = oklchColor(0.5, 0.1, 200);
const records = [];
for (const color of colors) {
	records.push({
		kind: "color",
		spec: spec(color),
		hex: colorToHex(color),
		ansi256: foregroundAnsi(color, "256color"),
		truecolor: foregroundAnsi(color, "truecolor"),
	});
	const okhsl = colorToOkhsl(color);
	records.push({ kind: "okhsl", spec: spec(color), h: okhsl.h, s: okhsl.s, l: okhsl.l });
	const oklch = colorToOklch(color);
	records.push({ kind: "oklch", spec: spec(color), l: oklch.l, c: oklch.c, h: oklch.h });
	for (const [space, first, second, amount] of [
		["srgb", color, red, 0.5],
		["srgb", green, color, 0.35],
		["oklch", color, base, 0.35],
	]) {
		records.push({
			kind: "mix",
			space,
			first: spec(first),
			second: spec(second),
			amount,
			hex: colorToHex(mixColors(first, second, amount, space)),
		});
	}
}

process.stdout.write(`${JSON.stringify(records, null, "\t")}\n`);
