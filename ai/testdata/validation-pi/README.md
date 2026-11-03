# pi-schema validation probes

`probes.json` is generated from pi v1.5.0 (`f9a26365`)
`packages/ai/test/validation-probes.test.ts`. It contains every probe whose
schema and arguments serialize to JSON, with the upstream outcome in `actual`.

Regenerate by adding this after `expected` in the upstream test and running
`vitest run packages/ai/test/validation-probes.test.ts`:

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
		writeFileSync("probes.json", JSON.stringify(rows, null, 1));
	});
});
```

The Go test compares the port's outcome with `actual`; do not hand-edit the
fixture.
