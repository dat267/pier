# Reproduced audit findings

## A475 a null identity version reads as an explicit empty version

- Severity: low (parity; malformed `User-Agent` header).
- Reference (pi v1.5.0 `f9a26365`, `utils/identity.ts:63`):
  `getClientVersion` is `identity?.version ?? PI_AI_VERSION`, and `??` treats
  `null` like absent, so `{"version":null}` yields the default version.
- Port: `ClientIdentity.UnmarshalJSON` sets `VersionSet = true` for a present
  `version` member whatever its JSON type, so a null version reads as the
  explicit empty version and `FormatUserAgent` renders `pi/ (linux; ...)`.
- Measured: `{"version":null}` -> port `pi/ (linux; go/1.27.3; amd64)`,
  reference `pi/1.5.0 (linux; node/...; x64)`.
- Also: `{"userAgent":{"mode":"custom","custom":null}}` yields an empty custom
  string where the reference passes a null `custom` through. That value is not
  a string either way, so it is not patched here.
- Fix: treat a `version` member that is JSON null as absent, keeping the
  explicit-empty case for `""`.
- Verification: a test over null, empty and set versions and the rendered
  header.
