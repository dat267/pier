# Reproduced, unresolved findings

The port matches the v1.6.0 pin for everything the goldens and tests cover.

## GitHub Copilot Responses tools go out without `strict` (#12286, `e5c01ca6c`)

- Severity: medium. Copilot's `/responses` treats a tool with no `strict` field as strict and fills omitted optional arguments with `""` or invented values, so pi's built-in tools (for example `read` with `offset`/`limit`) get wrong arguments.
- Fix shape: the catalog sets `compat["openai-responses"].supportsStrictMode = true` for every `github-copilot` model, which sends `strict: false` for ordinary tools and `strict: true` for JSON-schema tools.
- Blocked on: `ai/models_catalog.json` is generated, not hand-edited. Regenerating needs a scratch `./pi` copy and network (`node catalog/generate.ts --strict --data-only`), then the three `scripts/gen_*catalog.py` runs.

## Markdown rows after an inline image are drawn over (#12045, `5b4d7d3df`)

- Severity: low, and not reachable in pier today: a markdown image escape can only enter a message through an extension markdown transformer, which is out of scope (D41). Tool-result images render through their own component, not the markdown renderer.
- Revisit if a markdown transformer, or a LaTeX renderer that emits a terminal image, is ever supported: split a rendered line that contains an image on `\n` before the image check, so only the piece holding the image skips wrapping.
