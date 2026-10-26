// Reference: pi v1.4.0 (4a080252), session-search/oauth-selector/login-dialog.
import { component, legacyComponent, corpus, entries, emit, ui } from "./golden-reference.mjs";
const search = await component("session-selector-search");
const { OAuthSelectorComponent, formatAuthSelectorProviderType } = await legacyComponent("oauth-selector");
const { LoginDialogComponent } = await component("login-dialog");
const c = corpus("auth");
const sessions = c.sessions.map((s) => ({ ...s, path: s.id, allMessagesText: s.text, modified: new Date(s.modified) }));
for (const q of c.queries) {
	const p = search.parseSearchQuery(q);
	emit("a parse " + JSON.stringify(q), {
		mode: p.mode,
		tokens: p.tokens,
		error: p.error ?? null,
		regex: p.regex?.source ?? null,
	});
	emit(
		"a match " + JSON.stringify(q),
		sessions.map((s) => search.matchSession(s, p)),
	);
	for (const mode of ["threaded", "recent", "relevance"])
		emit(
			`a filter ${JSON.stringify(q)} ${mode} named`,
			search.filterAndSortSessions(sessions, q, mode, "named").map((s) => s.id),
		);
	emit(
		`a filter ${JSON.stringify(q)} relevance all`,
		search.filterAndSortSessions(sessions, q, "relevance", "all").map((s) => s.id),
	);
}
emit("a hasName", sessions.map(search.hasSessionName));
for (const [label, s] of entries(c.oauth)) {
	const v = new OAuthSelectorComponent(
		s.mode,
		s.providers,
		() => {},
		() => {},
		s.search,
	);
	for (const input of s.inputs ?? []) v.handleInput(input);
	emit(`a oauth ${label}@${s.width}`, v.render(s.width));
}
emit("a authtype", ["oauth", "api_key"].map(formatAuthSelectorProviderType));
for (const [label, s] of entries(c.login)) {
	const v = new LoginDialogComponent(ui, "openai", () => {});
	if (s.deviceCode?.length === 2) v.showDeviceCode({ verificationUri: s.deviceCode[0], userCode: s.deviceCode[1] });
	if (s.info?.length)
		v.showInfo(
			s.info[0],
			(s.info[1] ?? []).map(([url, label]) => ({ url, label })),
			s.info[2] ?? false,
		);
	if (s.waiting) v.showWaiting(s.waiting);
	if (s.details) v.showDetails(s.details);
	if (s.progress) v.showProgress(s.progress);
	if (s.prompt?.length === 2) void v.showPrompt(...s.prompt);
	emit(`a login ${label}@${s.width}`, v.render(s.width));
}
