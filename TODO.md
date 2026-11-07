# Reproduced audit findings

- M242 low/medium: pier's HTML export replaces template placeholders byte for
  byte, but the reference uses JS `String.replace`, so its own export is
  corrupted: the highlight bundle's `$&` expands to the literal
  `{{HIGHLIGHT_JS}}` text inside the embedded JS, and template.js's
  `$${totalCost.toFixed(3)}` collapses to `${totalCost.toFixed(3)}`, breaking the
  exported cost line. Documented as D385 before the reset; the record and its
  assertion were lost with the reset.
