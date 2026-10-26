// Reference: the pinned pi checkout's `semver` dependency, used by
// pi/packages/coding-agent/src/utils/version-check.ts (valid/compare).
// Regenerate from the repository root:
// node coding/testdata/semver/generate.mjs ./pi > coding/testdata/semver/golden.json
import { createRequire } from "node:module";
import { resolve } from "node:path";
const root = resolve(process.argv[2] ?? "./pi");
const { valid, compare } = createRequire(resolve(root, "package.json"))("semver");

const inputs = [
	"1.2.3", "v1.2.3", "=1.2.3", "01.2.3", "1.2", "1", "1.2.3-alpha", "1.2.3-alpha.1",
	"1.2.3-alpha.01", "1.2.3+build", "1.2.3+build.01", "1.2.3-0.3.7", "1.2.3-x.7.z.92",
	" 1.2.3 ", "1.2.3.4", "V1.2.3", "v 1.2.3", "1.2.3-", "1.2.3-alpha..1",
	"999999999999999999999.0.0", "1.0.0-rc.1+build", "1.2.3-ALPHA", "1.2.3-alpha_1",
	"1.2.3+", "1.2.3-0", "0.0.0", "", "v", "1.2.3-rc.1", "1.2.3-rc.1.0",
];
const pairs = [
	["1.2.3", "1.2.4"], ["1.2.3-alpha", "1.2.3"], ["1.2.3", "1.2.3-alpha"],
	["1.2.3-alpha.1", "1.2.3-alpha.beta"], ["1.2.3-alpha", "1.2.3-1"],
	["1.2.3-alpha", "1.2.3-alpha.1"], ["1.2.3+build", "1.2.3"],
	["1.2.3-2", "1.2.3-10"], ["0.0.1", "0.0.0"], ["1.2.3-rc.1", "1.2.3-rc.1+build"],
	["v1.2.3", "1.2.3"], [" 1.2.3 ", "v1.2.3"], ["10.0.0", "9.9.9"],
	["1.10.0", "1.9.0"], ["1.2.3-99999999999999999999", "1.2.3-100000000000000000000"],
	["1.2.3-alpha.beta", "1.2.3-alpha.1"],
];
const fixtures = {
	validity: inputs.map((input) => ({ input, valid: valid(input.trim()) ?? null })),
	pairs: pairs.map(([left, right]) => {
		const l = valid(left.trim());
		const r = valid(right.trim());
		return { left, right, comparison: l && r ? compare(l, r) : null };
	}),
};
console.log(JSON.stringify(fixtures, null, 2));
