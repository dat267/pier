# Reproduced audit findings

## Low

- CR-109: Responses conversion serializes a transcript with no emitted items as `null` instead of upstream's empty array `[]`.
