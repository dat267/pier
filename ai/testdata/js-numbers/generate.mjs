// Reference: pi v1.4.0 (4a080252), packages/ai/src/utils/json-stringify.ts and
// the JS number formatting the port mirrors (ai/jstext.go).
// Regenerate from repository root:
// node ai/testdata/js-numbers/generate.mjs > ai/testdata/js-numbers/golden.json
// Node provides String(number) and Number.prototype.toFixed directly, so the
// corpus records JavaScript's own output rather than a reimplementation.
const values = [
	0, 1, -1, 0.1, 1 / 3, 1e21, 1e-7, 5e-324, 1.7976931348623157e308,
	123456789012345680000, 1e-6, 999999999999999999999, 2 ** 53, 2 ** 53 + 2,
	1.005, 2.675, 0.615, 1.955, -1.005, 0.000001, 0.0000001, 1234.5678,
	1024.5, 1048576, 1048575, 51200, 1e20, 9.999999999999999e20,
];
for (let i = 1; i <= 40; i++) {
	values.push((i * 7919 + 13) / 1e7, -(i * 104729) / 1e3, i ** 7 / 7, 1 / (i * 977));
}
const output = values.map((value) => ({
	value: value.toString(),
	string: String(value),
	fixed: [0, 1, 2, 3].map((digits) => value.toFixed(digits)),
}));
console.log(JSON.stringify(output, null, 2));
