# Reproduced audit findings

- M135 (low): the shared JSON encoder rewrites a lone surrogate escape (`"\ud800"`) to U+FFFD, while `JSON.stringify(JSON.parse(...))` preserves the escape. Go strings cannot hold a lone surrogate, so the replacement is unavoidable at that layer, but the divergence from D253's integer rule was undocumented and untested.
