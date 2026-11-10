# Reproduced audit findings

## A470 `JSSlice` UTF-16-encodes the whole input to return a short prefix

- Severity: medium (measured; provider error path, ID sanitizing, faux chunks).
- Measured `ai.JSSlice(body, 0, 500)` prefix of a body, old implementation
  against new (same process, 20 reps each):

  | body | before | after | speedup |
  |---|---|---|---|
  | 64 KB | 372 µs | 6.0 µs | 62x |
  | 1 MB | 5.71 ms | 10.0 µs | 574x |
  | 8 MB | 34.8 ms | 5.5 µs | 6308x |

  Allocations for the 500-char prefix of an 8 MB body: 6 -> 5. The remaining
  cost is flat in the body size, which is the point.
  Reference: `ownedSlice` (pi v1.5.0 `packages/ai/src/utils/text.ts:48`) is
  O(1) in the returned length because V8's `slice` is a view.
- Reachable in production: `TruncateErrorText` (`ai/errorbody.go:89`,
  reference `utils/error-body.ts:139`, cap 500), `truncateHTTPBody` (radius,
  512), `truncateErrorText` (mistral), `SanitizeID` (64), `pi_messages.go`
  (maxLength). `fauxSplitStringByTokenSize` calls it once per emitted chunk.
- Cause: `JSSlice` did `utf16.Encode([]rune(s))` over the whole string before
  clamping, so every call was O(len(s)) in time and allocation.
- Fix: `jsSliceAbsolute` walks only up to `end` when both indices are
  non-negative, and appends each UTF-16 unit by index so a cut inside a
  surrogate pair still decodes to U+FFFD like the full encode. The negative-index
  path keeps the full encode, since it needs the string's UTF-16 length.
- Verification: `TestJSSliceMatchesFullEncoding` (differential against the old
  implementation over ASCII, astral, combining and invalid-UTF-8 inputs across
  negative, zero, in-range and out-of-range index pairs) and
  `TestJSSlicePrefixDoesNotScanTheWholeString` (allocation ceiling).
