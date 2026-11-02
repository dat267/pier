# Reproduced audit findings

- M132 (low): MCP OAuth metadata validation rejects only the `javascript:`, `data:` and `vbscript:` schemes, while pinned pi v1.5.0 accepts only http(s) URLs for every metadata URL (authorization/token/registration endpoints, issuer, resource, authorization servers). A `file:` or custom-scheme endpoint therefore passes discovery validation, and the authorization endpoint is one the client later hands to the platform URL handler.
