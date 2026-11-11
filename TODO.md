# Reproduced audit findings
- **A520** (medium) Three v1.5.2 compatibility flags are missing, so a model config that sets them is ignored.
  - Where: `ai/models.go` (compat structs), `coding/modelconfig.go` (compat field registry), the OpenAI completions/responses wire builders.
  - Scenario: the pin's `api/compat-schema.ts` gained `requiresNonNullAssistantContent`, `requiresToolsWithToolHistory` and `promptCacheKeyMode` (`auto`/`enabled`/`disabled`). A user whose model entry sets one gets pier's default wire instead: a replayed tool-call-only assistant message sends `"content": null` where some providers need `""`; a request without tools omits `"tools"` where older LiteLLM proxies in front of Anthropic reject the history; and `prompt_cache_key` is always sent, where a proxy that rejects it has no way to disable it.
  - Evidence: none yet; the fields do not exist in pier, so a config setting them parses as an unknown key.
  - Root cause: the flags arrived with the v1.5.2 bump, which ported none of the `ai` compat schema.
  - Repair plan: add the three fields with the pin's descriptions, thread them through `coding/modelconfig.go`, honor them in the wire builders, and pin each with a wire test citing `providers/compat-schema.ts`.
