# Compaction audit reproductions

These are intentionally failing probes for the unresolved findings in
`TODO.md`, audited against pi v1.3.0 (`71023aec`). They use existing package
`coding` test helpers, so copy the file into that package to run them. Keeping
it under `testdata` leaves the normal test suite unchanged.

From the repository root:

```bash
(
    set -eu
    probe=coding/compaction_audit_probe_test.go
    test ! -e "$probe"
    trap 'rm -f "$probe"' EXIT
    cp coding/testdata/compaction-audit/compaction_audit_probe_test.go "$probe"
    go test ./coding -run '^TestAuditCompaction' -count=3
)
```

Expected on the audited code: all five top-level tests fail, including both
manual and automatic subtests for the selected reasoning level. The failures
assert externally observable estimates, request options, or prompt behavior;
no real provider requests are made.

When fixing a finding, promote its probe into the normal test suite, verify
red then green, and remove its entry from `TODO.md`.
