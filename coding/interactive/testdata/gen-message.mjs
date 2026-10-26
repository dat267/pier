// Reference: pi v1.4.0 (4a080252), message components and status-indicator.ts.
import { component, corpus, entries, emit, ui } from "./golden-reference.mjs";
const { AssistantMessageComponent } = await component("assistant-message");
const { BranchSummaryMessageComponent } = await component("branch-summary-message");
const { CompactionSummaryMessageComponent } = await component("compaction-summary-message");
const status = await component("status-indicator");
const c = corpus("message");
for (const [label, s] of entries(c.assistant)) {
	const v = new AssistantMessageComponent(
		{
			role: "assistant",
			content: s.content,
			api: "openai",
			provider: "openai",
			model: "gpt",
			stopReason: s.stopReason ?? "stop",
			errorMessage: s.errorMessage,
		},
		s.hideThinking ?? false,
	);
	emit(`m assistant ${label}@${s.width}`, v.render(s.width));
	if (s.expandThinking != null) {
		v.setHideThinkingBlock(s.expandThinking);
		emit(`m assistant ${label}-toggled@${s.width}`, v.render(s.width));
	}
}
for (const [kind, Class] of [
	["branch", BranchSummaryMessageComponent],
	["compaction", CompactionSummaryMessageComponent],
]) {
	for (const [label, s] of entries(c[kind])) {
		const v = new Class({ role: kind + "Summary", summary: s.summary, tokensBefore: 1234 });
		emit(`m ${kind} ${label}@${s.width}`, v.render(s.width));
		v.setExpanded(true);
		emit(`m ${kind} ${label}-expanded@${s.width}`, v.render(s.width));
	}
}
const working = new status.WorkingStatusIndicator(ui, "Working...");
working.dispose();
emit("m status working", [working.render(30), working.renderInBorder(20), working.renderSpinnerInBorder(5)]);
for (const [kind, v] of [
	["retry", new status.RetryStatusIndicator(ui, 2, 5, 3000)],
	["compaction", new status.CompactionStatusIndicator(ui, "threshold")],
	["branch", new status.BranchSummaryStatusIndicator(ui)],
]) {
	v.dispose();
	emit("m status " + kind, v.render(60));
}
emit("m status idle", new status.IdleStatus().render(8));
