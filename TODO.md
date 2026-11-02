# Reproduced audit findings

- M127 (low): a non-web login link or device URL is displayed through `SanitizeUntrustedText`, which keeps newlines and tabs, so a provider-supplied value with a control run can break the dialog into extra lines. Pinned pi v1.5.0's `displayUrl` replaces each C0/C1 control run with a single space (utils.ts `replaceControlCharacters`).
