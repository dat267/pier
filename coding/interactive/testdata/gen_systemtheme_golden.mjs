// Regenerates testdata/systemtheme_golden.json by driving the pinned upstream
// system-theme.ts (modes/interactive/theme/system-theme.ts) with Node.
//
//	# in the repo root
//	node coding/interactive/testdata/gen_systemtheme_golden.mjs \
//		> coding/interactive/testdata/systemtheme_golden.json
//
// The upstream file imports "@earendil-works/pi-tui", which Node cannot resolve
// without an installed workspace. Create the shim once (pi is the gitignored
// upstream checkout):
//
//	mkdir -p pi/node_modules/@earendil-works/pi-tui
//	echo '{"name":"@earendil-works/pi-tui","type":"module","main":"index.js"}' \
//		> pi/node_modules/@earendil-works/pi-tui/package.json
//	cat > pi/node_modules/@earendil-works/pi-tui/index.js <<'EOF'
//	export { colorToOkhsl, colorToOklch, colorToRgb, okhslColor, oklchColor, rgbColor }
//	  from "../../../packages/tui/src/colors.ts";
//	export { oklabToOkhslLightness } from "../../../packages/tui/src/oklab.ts";
//	EOF
//
// The shim must be JavaScript: Node refuses to strip types under node_modules,
// while the re-export targets (packages/tui/src/*.ts) strip fine.
const root = "/home/dat/repos/pier";
const { generateSystemThemeColors } = await import(
	`${root}/pi/packages/coding-agent/src/modes/interactive/theme/system-theme.ts`
);
const rgb = (hex) => {
	const value = parseInt(hex.replace("#", ""), 16);
	return { r: (value >> 16) & 0xff, g: (value >> 8) & 0xff, b: value & 0xff };
};
const paletteHexes = [
	"#21222c", "#ff5555", "#50fa7b", "#f1fa8c", "#bd93f9", "#ff79c6", "#8be9fd", "#f8f8f2",
	"#6272a4", "#ff6e6e", "#69ff94", "#ffffa5", "#d6acff", "#ff92df", "#a4ffff", "#ffffff",
];
const palette = paletteHexes.map(rgb);
const cases = {
	dracula: { background: rgb("#282a36"), foreground: rgb("#f8f8f2"), palette },
	draculaGray: { background: rgb("#282a36"), foreground: rgb("#f8f8f2"), palette, saturation: 0 },
	solarizedLight: { background: rgb("#fdf6e3"), foreground: rgb("#657b83") },
	backgroundOnly: { background: rgb("#1e1e1e") },
	midGray: { background: rgb("#808080"), foreground: rgb("#ffffff") },
	noColorsLight: { appearanceHint: "light" },
	noColorsGray: { saturation: 0 },
};
const out = {};
for (const [name, input] of Object.entries(cases)) {
	const result = generateSystemThemeColors(input);
	out[name] = { colors: result.colors, dim: result.dim, appearance: result.appearance };
}
process.stdout.write(JSON.stringify(out, null, "\t"));
