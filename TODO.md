# TODO

Reproduced, unresolved findings go here, each with its reproduction, why it is not
fixed, and the tests that must stay green; the file is empty when none are open.

## F-M112: `GetPiUserAgent` is dead code and its test cannot fail

`ai.GetPiUserAgent` (`ai/jstext.go`) renders `"pi (<platform> <release>; <arch>)"`.
Nothing in the tree calls it except `TestGetPiUserAgent`
(`ai/anthropic_stream_retry_test.go:255`), so it is dead production code. Its
doc comment claimed to port `utils/pi-user-agent.ts`, but that reference function
returns `formatUserAgent(...)`'s `"pi/<version> (<platform>; node/<version>;
<arch>)"`, which pier implements as `FormatUserAgent` (D265). The comment is
corrected; the dead cluster is not deleted.

Reproduction: `grep -rn GetPiUserAgent --include=*.go .` shows only the
definition, its test, and no callers.

Removing it also orphans `kernelRelease` and the `runtimeGOOS`/`runtimeGOARCH`
twins in `ai/useragent_unix.go` and `ai/useragent_other.go`, i.e. four files.
That is a deletion of build-tagged files, which is the maintainer's call rather
than a repair to make unasked.

Also weak: `TestGetPiUserAgent` asserts `HasPrefix(ua, "pi (")` and
`HasSuffix(ua, ")")`, and the function's format always ends in `)`, so the test
can only fail on the platform/release part. It does not pin the actual string.

Tests that must stay green if the cluster is removed:
`TestFormatUserAgent`, `TestLiteralUserAgent`,
`TestRequestIdentityHeaders`, `TestImageRequestIdentityHeaders`.
