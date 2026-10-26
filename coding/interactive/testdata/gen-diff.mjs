// Reference: pi v1.4.0 (4a080252), components/diff.ts and diff package.
import { component, corpus, entries, emit } from "./golden-reference.mjs";
import { createRequire } from "node:module";
import { resolve } from "node:path";
import { root } from "./golden-reference.mjs";
const { diffWords } = createRequire(resolve(root, "package.json"))("diff");
const { renderDiff } = await component("diff");
const c = corpus("diff");
for (const [label, [a, b]] of entries(c.words))
	emit(
		"d words " + label,
		diffWords(a, b).map((v) => ({ value: v.value, added: !!v.added, removed: !!v.removed })),
	);
for (const [label, text] of entries(c.renders)) emit("d render " + label, renderDiff(text));
