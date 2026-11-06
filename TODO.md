# Reproduced audit findings

- M227 (low): a skill whose frontmatter uses a YAML block scalar (`description: |`) is dropped with a "description is required" warning, while pi loads it (upstream fixture `skills/multiline-description`); the other 12 upstream skill fixtures match.
