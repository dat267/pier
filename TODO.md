# Reproduced audit findings

- M111 (low): the session picker groups forked sessions by a canonical path compared case-sensitively, so on Windows a parent session whose stored path differs only in case (`c:\Users` vs `C:\Users`) is not linked and the tree shows separate roots. Pinned pi v1.5.0 compares `getPathComparisonKey(canonicalizePath(path))`, which also folds case on Windows (session-selector.ts).
