# Reproduced audit findings

## A469 `scanJSONStringStrict` byte-at-a-time validation is over half the session-list cost

- Severity: performance (responsive startup/list path).
- Measured workload: `buildSessionInfo` over the real 60-file corpus under
  `~/.pi/agent/sessions` (503 MB total, 6.5 MB retained text). Go median
  2.21 s; the reference `SessionManager.listAll` over the same files 1.97 s.
  Single 260 MB session, single file, no concurrency: Go median 1.16 s,
  reference 0.96 s. Go is 13-20% slower on both.
- Profile (`/tmp/cpu2.prof`, 4 runs of the 260 MB session): flat 54.6%
  `coding.scanJSONStringStrict`, called only from `scanJSONValueAt`.
- Cause: `scanJSONValueAt` dispatches every string to `scanJSONStringStrict`,
  which loops one byte at a time with a 4-way `switch`. Every non-ASCII byte
  also falls into `default`, so UTF-8 continuation bytes are re-examined.
  `scanPlainStringBytes` already exists and never validates UTF-8 because it
  bails to the decoder on any backslash or control byte.
- Fix: `scanJSONStringPlainRun` skips plain runs eight bytes at a time and the
  strict scanner only enters its byte-at-a-time switch at a quote, backslash or
  control byte. Same accept set.
- Verification: `TestSessionScanAcceptsOnlyValidJSON` and
  `FuzzSessionScanAcceptsOnlyValidJSON` pin the accept set against
  `encoding/json`; a byte-equivalence test pins the two scanners; the corpus
  benchmark must beat the reference median.
