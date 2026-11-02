# Reproduced audit findings

- M103 (low): native Shift+Enter detection ignores the ESC+CR form that some Apple Terminal setups send (pi v1.5.0 terminal.ts #11559), so those terminals insert a newline instead of a soft one. Pier's `NormalizeNativeShiftEnterInput` and the `forwardInputSequence` detection both test only `\r`.
