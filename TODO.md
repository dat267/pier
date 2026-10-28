# Reproduced audit findings

## Low

- CR-111: Replaying a grammar tool with a valid empty-string input property treats it as a function call (and Responses result) because converters confuse the empty property name with an absent map entry.
