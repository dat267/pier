# Reproduced audit findings

## P3

- **A407 (M256)**: `isFenceEnd` requires the opener's character run to be followed by whitespace only, so `'```\na\n```~~~\nb'` keeps `b` and the fence line inside the code block; marked's closing-fence pattern (`{0,3}\1[~`]* *(?=\n|$)`) allows a mixed backtick/tilde run after the opener and closes the block, rendering `b` as a paragraph. It also accepts a trailing tab, which marked does not.
