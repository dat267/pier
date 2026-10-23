# Compaction audit reproductions

These are intentionally failing probes for the unresolved findings in
`TODO.md`, audited against pi v1.3.0 (`71023aec`). They use existing package
`coding` test helpers, so copy the files into that package to run them. Keeping
them under `testdata` leaves the normal test suite unchanged.

From the repository root:

```bash
(
    set -eu
    probe=coding/compaction_audit_probe_test.go
    followup=coding/compaction_followup_probe_test.go
    test ! -e "$probe"
    test ! -e "$followup"
    trap 'rm -f "$probe" "$followup"' EXIT
    cp coding/testdata/compaction-audit/compaction_audit_probe_test.go "$probe"
    cp coding/testdata/compaction-audit/compaction_followup_probe_test.go "$followup"
    go test ./coding -run '^TestAuditCompaction' -count=3
)
```

Expected on the audited code: all eight top-level tests fail, including both
manual and automatic subtests for the selected reasoning level and auth
cancellation. The failures assert externally observable estimates, request
options, file-list order, or prompt behavior; no real provider requests are made.

When fixing a finding, promote its probe into the normal test suite, verify
red then green, and remove its entry from `TODO.md`.
