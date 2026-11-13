# pi-schema validation probes

`probes.json` is generated from pi v1.5.2 (`b4afdddf`)
`packages/ai/test/validation-probes.test.ts`. It contains every probe whose
schema and arguments serialize to JSON, with the upstream outcome in `actual`.

Regenerate inside the pinned checkout, restoring it afterwards:

1. `npm run build --workspace @earendil-works/pi-schema` (the test imports the
   package's `dist`, which `npm ci` alone does not build).
2. Add the block below after `expected` in
   `packages/ai/test/validation-probes.test.ts`, pointing `writeFileSync` at a
   path outside the checkout, then run
   `node_modules/.bin/vitest run packages/ai/test/validation-probes.test.ts -t dumps`.
3. Restore the test file (`git -C . checkout -- packages/ai/test/validation-probes.test.ts`)
   and confirm `git status --porcelain` is empty.

The block:

```ts
import { writeFileSync } from "node:fs";
describe("dump probes", () => {
	it("dumps", () => {
		const rows = all.flatMap(([key, name, schema, arg]) => {
			try {
				const schemaText = JSON.stringify(schema);
				const argText = JSON.stringify(arg);
				if (schemaText === undefined || argText === undefined) return [];
				return [{
					key,
					name,
					schema: JSON.parse(schemaText),
					arg: JSON.parse(argText),
					expected: expected[key],
					actual: outcome(name, schema, arg),
				}];
			} catch {
				return [];
			}
		});
		writeFileSync("/tmp/probes-v152.json", JSON.stringify(rows, null, 1));
	});
});
```

The Go test compares the port's outcome with `actual`; do not hand-edit the
fixture.
