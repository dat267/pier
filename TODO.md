# Reproduced audit findings

- CR-168: Low — highlighting relevance still differs from the pinned engine in five languages, so `highlightAuto` can score them differently: erlang -1 per fixture, javascript -12 over 12 mutated cases, perl -42 (e.g. `my $interp = "name=$name count=$count";` gives 3, reference 5), markdown -3 on one case, ruby -1 on an indented heredoc body. Evidence: `/tmp/pier-fresh-diff.json` (2,304 mutated cases, 191 languages) and `/tmp/pier-3am-review-notes.md`; every other language now agrees after CR-167.
