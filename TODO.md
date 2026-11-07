# Reproduced audit findings

- M249 blocked (needs a product decision): the reference's startup settings
  manager default-trusts the project scope, so a project `.pi/settings.json`
  `sessionDir` applies at startup and an unreadable project settings file is
  reported before the TUI. Pier keeps the startup manager project-untrusted so
  no project value is read before the trust decision. Documented as D393;
  adopting the reference behavior would read untrusted project settings, so it
  needs an explicit decision.
