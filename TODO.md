# Reproduced audit findings

## Medium

- CR-123: MCP tool-list change notifications reuse the completed connect's cancelled context, so live connections fail every later tool refresh with `MCP request aborted` and never publish updated tools.

## Low

- CR-117: Highlighter multiline anchors use RE2's LF-only boundaries instead of JavaScript's CR/LF/LS/PS boundaries, so comments, directives, and headings can consume later lines or include the CR of CRLF files.
