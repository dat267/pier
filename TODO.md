# Reproduced audit findings

- M136 (low): the session-search relevance benchmark's ranking assertion broke with the v1.5.0 search tiers: both "needle" and "prefix needle" are now equal-tier text matches, so relevance falls back to recency and the benchmark's expected row is wrong (it fails under `go test -bench`). Evidence: `BenchmarkSessionSearchRelevance` fails at every size.
