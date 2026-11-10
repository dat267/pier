# Reproduced audit findings

## A474 null custom-message content becomes an empty text block in the request

- Severity: low (parity; an extra empty block in a provider request).
- Reference (pi v1.5.0 `f9a26365`, `core/messages.ts:163`):
  `const content = typeof m.content === "string" ? [{type:"text", text:m.content}] : m.content;`
  `typeof null === "object"`, so a null content is passed through as-is and adds
  no text block.
- Port: `customContentToBlocks` (`coding/converttollm.go:220`) decodes into a
  Go string first, and `json.Unmarshal([]byte("null"), &text)` succeeds as "",
  so a null content becomes one empty text block.
- Measured, `ConvertToLlm` on `{"customType":"note","content":null}`:
  port `[{"type":"text","text":""}]`, reference no block.
- Reachable: a custom message written with a null content (an extension or a
  hand-edited session) puts an extra empty block in every following request.
- Fix: require the raw member to actually be a JSON string before decoding it
  as text, then fall through to the block and raw-text paths.
- Verification: a test over null, empty string, plain string and block content.
