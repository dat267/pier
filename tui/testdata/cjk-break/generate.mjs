// Prints the code-point ranges the reference's cjkBreakRegex matches, so the
// port's hand-enumerated table is checked against the pin.
//
//	node tui/testdata/cjk-break/generate.mjs
const reference = /[\p{Script_Extensions=Han}\p{Script_Extensions=Hiragana}\p{Script_Extensions=Katakana}\p{Script_Extensions=Hangul}\p{Script_Extensions=Bopomofo}]/u;
const ranges = [];
let start = -1;
for (let codePoint = 0; codePoint <= 0x10ffff; codePoint++) {
	if (reference.test(String.fromCodePoint(codePoint))) {
		if (start < 0) start = codePoint;
	} else if (start >= 0) {
		ranges.push([start, codePoint - 1]);
		start = -1;
	}
}
if (start >= 0) ranges.push([start, 0x10ffff]);
console.log(JSON.stringify(ranges));
