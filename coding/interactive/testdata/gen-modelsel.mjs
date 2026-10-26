// Reference: pi v1.4.0 (4a080252), model-selector.ts/scoped-models-selector.ts.
import { component, corpus, entries, emit, ui } from "./golden-reference.mjs";
const { ModelSelectorComponent } = await component("model-selector");
const { ScopedModelsSelectorComponent } = await component("scoped-models-selector");
const models = [
	{ provider: "openai", id: "gpt-5", name: "GPT-5", api: "openai", reasoning: true },
	{ provider: "openai", id: "gpt-4o", name: "GPT-4o", api: "openai" },
	{ provider: "anthropic", id: "claude-3", name: "Claude 3", api: "anthropic", reasoning: true },
];
const runtime = {
	getAvailableSnapshot: () => models,
	getModel: (p, id) => models.find((m) => m.provider === p && m.id === id),
	getError: () => undefined,
	refresh: () => new Promise(() => {}),
};
const c = corpus("modelsel");
for (const [label, s] of entries(c.model)) {
	const v = new ModelSelectorComponent(
		ui,
		s.current == null ? undefined : models[s.current],
		runtime,
		(s.scoped ?? []).map((i) => ({ model: models[i] })),
		() => {},
		() => {},
		s.search,
		s.selectAsDefault ? () => {} : undefined,
		s.default == null ? undefined : { provider: models[s.default].provider, id: models[s.default].id },
	);
	for (const i of s.inputs ?? []) v.handleInput(i);
	emit(`m modelsel ${label}@${s.width}`, v.render(s.width));
	v.dispose();
}
for (const [label, s] of entries(c.scoped)) {
	const v = new ScopedModelsSelectorComponent(
		{
			allModels: models,
			enabledModelIds: s.enabled == null ? null : s.enabled.map((i) => models[i].provider + "/" + models[i].id),
			refreshStatus: s.refreshStatus,
		},
		{ onChange() {} },
	);
	for (const i of s.inputs ?? []) v.handleInput(i);
	emit(`m scoped ${label}@${s.width}`, v.render(s.width));
}
