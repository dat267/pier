# Reproduced audit findings

## A473 `schemaTypes` drops a `type` array with any non-string member

- Severity: medium (parity; provider request differs from the reference).
- Reference (pi v1.5.0 `f9a26365`,
  `packages/ai/src/api/constrained-sampling.ts:40`):
  `const types = typeof schema.type === "string" ? [schema.type] : Array.isArray(schema.type) ? schema.type : [];`
  A mixed array keeps its string members, so
  `isStructuredSchema({"type":["object",5]})` is `true` and the `anyOf` guard
  throws `object and array unions are unsupported`.
- Port: `schemaTypes` (`ai/constrained_sampling.go:22`) decodes into
  `[]string`; a single non-string member fails the decode and the whole array
  becomes `nil`, so `isStructuredSchema` answers `false` and the guard passes.
- Measured, same nested schema:
  `{"anyOf":[{"type":["object",5]}]}` -> port err=`nil`, reference throws
  `object and array unions are unsupported`.
  `{"anyOf":[{"type":"object"}]}` -> both throw (the control).
- Reachable: a tool's JSON schema is model- or extension-supplied; it reaches
  `MakeStrictJSONSchema` for providers using strict structured output.
- Fix: decode the array as raw members, keep the ones that are strings, and
  drop the rest, matching JS exactly.
- Verification: a test asserting the guard now rejects the mixed array, with
  the plain-object variant as the control.
