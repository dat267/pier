# Reproduced audit findings

- M124 (medium): Radius OAuth trusts the gateway's `authorizationEndpoint` and its device-authorization `verification_uri` as raw strings. Both are opened with the platform URL handler, so a hostile or compromised gateway can return a `file:` or custom-scheme URL that launches a local program. Pinned pi v1.5.0 routes both through `trustedHttpUrl` (radius.ts, trusted-url.ts), which accepts only normalized http(s) URLs.
