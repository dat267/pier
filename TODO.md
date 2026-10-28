# Reproduced audit findings

- CR-153: R can start a single-quoted string where upstream does not, so the root illegal `->` is not reached: for the seeded random `r-5` input the port highlights the string while upstream aborts to plain text.
