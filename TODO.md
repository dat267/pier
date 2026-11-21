# Reproduced, unresolved findings

The port matches the v1.6.0 pin for everything the goldens cover. These v1.6.0
fixes are in packages pier ports but are not ported yet; each names its commit.

## GitHub Copilot Responses tools go out without `strict` (#12286, `e5c01ca6c`)

- Severity: medium. Copilot's `/responses` treats a tool with no `strict` field as strict and fills omitted optional arguments with `""` or invented values, so pi's built-in tools (for example `read` with `offset`/`limit`) get wrong arguments.
- Fix shape: the catalog sets `compat["openai-responses"].supportsStrictMode = true` for every `github-copilot` model, which sends `strict: false` for ordinary tools and `strict: true` for JSON-schema tools.
- Blocked on: `ai/models_catalog.json` is generated, not hand-edited. Regenerating needs a scratch `./pi` copy and network (`node catalog/generate.ts --strict --data-only`), then the three `scripts/gen_*catalog.py` runs.

## Markdown rows after an inline image are drawn over (#12045, `5b4d7d3df`)

- Severity: low. A paragraph holding a multi-row terminal image (Kitty/iTerm2) plus spacer rows joined by hard breaks renders as one row, so following content is drawn over the image.
- Fix shape: split a rendered line that contains an image on `\n` before the image check, so only the piece holding the image skips wrapping.

## `/login` and `/logout` search picks the first match, not the best (#12246, `37b74c546`)

- Severity: low. The OAuth, scoped-model and session selectors keep the first fuzzy match where the reference picks the best-scoring one.
- Fix shape: rank candidates with the reference's scoring instead of returning the first hit.
