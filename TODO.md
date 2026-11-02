# Reproduced audit findings

- M79 (medium): `IsContextOverflow` lacks the LiteLLM `context window exceeded` pattern that pinned pi v1.5.0 added to `utils/overflow.ts`. A `litellm.ContextWindowExceededError` is not classified as overflow, so pier neither auto-compacts nor runs the compact-and-retry recovery and the raw provider error reaches the user. Evidence: `/tmp/pier-maintenance-20261102-notes.md` M79.
- M80 (low): `IsRetryableAssistantError` lacks the Zhipu/Z.ai error code 1308 non-retryable limit pattern that pinned pi v1.5.0 added to `utils/retry.ts`. A plan usage limit sent as HTTP 429 is treated as a transient rate limit and retried with backoff even though it resets after hours. Evidence: `/tmp/pier-maintenance-20261102-notes.md` M80.
