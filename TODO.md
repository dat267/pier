# Reproduced audit findings

- CR-152: AutoHotkey hotkey titles stop early: for `^!s::*\n-(** 0^!s::=+(b\n`, upstream scopes `-(** 0^!s:` as a title while the port scopes only the number.
- CR-153: R can start a single-quoted string where upstream does not, so the root illegal `->` is not reached: for the seeded random `r-5` input the port highlights the string while upstream aborts to plain text.
