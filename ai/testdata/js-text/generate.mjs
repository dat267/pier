// Reference: pi v1.5.2 (b4afdddf) utils/text.ts, which formats strings and
// numbers through JavaScript itself. This generator records Node's results for
// the helpers in ai/jstext.go; it needs no reference checkout. Run from the
// repository root:
//   node --experimental-strip-types ai/testdata/js-text/generate.mjs ./pi > ai/testdata/js-text/golden.json
// fmt: off
const numbers = [0, -0, 1, -1, 0.5, 1.5, 2.5, -0.5, 1.005, 2.675, 0.0001, -0.0001, 1e-7, 1.5e-7, 1e-6, 1e20, 1e21, -1e21, 1.2345678901234567e20, 123456789, 999999.5, 1/3, 2/3, 1e300, 5e-324, 1.7976931348623157e308, 100, 0.1+0.2, 3.14159, 1e-5, 1234.5678];
const digits = [0, 1, 2, 3, 6];
const fixed = [];
for (const n of numbers) for (const d of digits) fixed.push({ n, d, out: n.toFixed(d) });
const strings = numbers.map((n) => ({ n, out: String(n) }));
const lower = ["ΑΣ", "İSTANBUL", "ẞ", "ǅ", "ΣΣ", "AΣ", "ΓΛΩΣΣΑ", "ABC", "aA", "日本語", "ﬃ", "ŒUVRE", "İ", "I", "ı"];
const lowers = lower.map((s) => ({ s, out: s.toLowerCase() }));
const sliceStrings = ["abcdef", "日本語", "😀x", "a😀b", "e\u0301x", ""];
const slices = [];
for (const s of sliceStrings) for (const a of [-5, -2, -1, 0, 1, 2, 3, 5]) for (const b of [-5, -2, -1, 0, 1, 2, 3, 5]) slices.push({ s, a, b, out: s.slice(a, b) });
const trims = [" a ", "\u00a0a\u3000", "\uFEFFx\u2028", "\v\tx\r\n", "a\u0085"].map((s) => ({ s, out: s.trim() }));
process.stdout.write(`${JSON.stringify({ fixed, strings, lowers, slices, trims }, null, 1)}\n`);
