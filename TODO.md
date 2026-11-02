# Reproduced audit findings

- M125 (medium): Gemini function declarations send tool JSON Schema `const` unchanged in `parametersJsonSchema`, but Gemini ignores `const`; pinned pi v1.5.0 rewrites string and number `const` values to single-value `enum`s (google-shared.ts `rewriteConstAsEnum`), so a const-constrained parameter (common in MCP tool schemas) loses its constraint on Gemini.
