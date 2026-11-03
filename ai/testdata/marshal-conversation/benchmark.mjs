// Node reference for BenchmarkMarshalConversationRealistic. Generate inputs
// with TestWriteRealisticConversationFixtures, then:
//
//	node ai/testdata/marshal-conversation/benchmark.mjs /tmp/pier-bench
//
// JSON.stringify must reproduce the Go canonical bytes byte-for-byte; the
// script aborts otherwise. Times are milliseconds.
import fs from "node:fs";
import path from "node:path";

const dir = process.argv[2];
if (!dir) throw new Error("usage: benchmark.mjs <fixture-directory>");
const iterations = {100: 500, 500: 200, 4000: 40};

for (const n of [100, 500, 4000]) {
	const file = path.join(dir, `realistic-${n}.json`);
	const text = fs.readFileSync(file, "utf8");
	const value = JSON.parse(text);

	let output = "";
	for (let i = 0; i < 50; i++) output = JSON.stringify(value);
	if (output !== text) {
		throw new Error(`${n}: JSON.stringify differs from the Go canonical bytes`);
	}

	const times = [];
	for (let i = 0; i < iterations[n]; i++) {
		const start = performance.now();
		const encoded = JSON.stringify(value);
		const elapsed = performance.now() - start;
		if (encoded.length !== text.length) throw new Error(`${n}: length drift`);
		times.push(elapsed);
	}
	times.sort((a, b) => a - b);
	const median = times[Math.floor(times.length / 2)];
	const mean = times.reduce((a, b) => a + b, 0) / times.length;
	console.log(`${n}: median ${median.toFixed(3)} ms mean ${mean.toFixed(3)} ms bytes ${text.length}`);
}
