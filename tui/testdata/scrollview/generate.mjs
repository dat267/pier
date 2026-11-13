// pi v1.5.2 (b4afdddf), packages/tui/src/components/scroll-view.ts. Drives the
// reference ScrollView through a scripted layout/scroll sequence and prints the
// observable state after each step: scrollTop, follow-end, viewport height,
// scrollbar visibility and activity, render requests, and the rendered lines.
// Read-only source load; run from the repository root:
//
//	node tui/testdata/scrollview/generate.mjs ./pi > tui/testdata/scrollview/golden.json
import { resolve } from "node:path";

const root = resolve(process.argv[2] ?? "./pi");
const { ScrollView } = await import(new URL(`file://${root}/packages/tui/src/index.ts`).href);

// ScriptedComponent mirrors tui/mainscreen_test.go's scriptedComponent: it pads
// each line to the render width by rune count.
class ScriptedComponent {
	constructor(lines) {
		this.lines = lines;
	}
	render(width) {
		return this.lines.map((line) => {
			const runes = Array.from(line);
			return runes.length >= width ? runes.slice(0, width).join("") : line + " ".repeat(width - runes.length);
		});
	}
	invalidate() {}
}

const lines = (prefix, count) => Array.from({ length: count }, (_, index) => `${prefix}${index}`);

const cases = {
	"follow-none-scroll": {
		options: {},
		lines: lines("line", 12),
		steps: [
			{ op: "layout", contentHeight: 12, viewportHeight: 5 },
			{ op: "scrollBy", lines: 3 },
			{ op: "scrollBy", lines: 100 },
			{ op: "scrollBy", lines: -2 },
			{ op: "scrollTo", top: 4 },
			{ op: "start" },
			{ op: "end" },
		],
	},
	"follow-end-grows": {
		options: { follow: "end" },
		lines: lines("line", 12),
		steps: [
			{ op: "layout", contentHeight: 4, viewportHeight: 5 },
			{ op: "layout", contentHeight: 12, viewportHeight: 5 },
			{ op: "scrollBy", lines: -2 },
			{ op: "layout", contentHeight: 20, viewportHeight: 5 },
			{ op: "scrollBy", lines: 100 },
			{ op: "scrollBy", lines: -1 },
			{ op: "end" },
		],
	},
	"disable-follow-at-end": {
		options: { follow: "end" },
		lines: lines("line", 12),
		steps: [
			{ op: "layout", contentHeight: 12, viewportHeight: 5 },
			{ op: "scrollTo", top: 7, disableFollow: true },
			{ op: "layout", contentHeight: 12, viewportHeight: 5 },
			{ op: "scrollTo", top: 7 },
			{ op: "scrollTo", top: 99 },
		],
	},
	"clamp-and-shorten": {
		options: {},
		lines: lines("line", 12),
		steps: [
			{ op: "layout", contentHeight: 12, viewportHeight: 5 },
			{ op: "scrollTo", top: 7 },
			{ op: "layout", contentHeight: 6, viewportHeight: 5 },
			{ op: "layout", contentHeight: 6, viewportHeight: 9 },
			{ op: "layout", contentHeight: -3, viewportHeight: -1 },
		],
	},
	"scrollbar-auto": {
		options: { scrollbar: "auto" },
		lines: lines("line", 12),
		steps: [
			{ op: "layout", contentHeight: 12, viewportHeight: 5 },
			{ op: "scrollBy", lines: 1 },
			{ op: "render", width: 20 },
			{ op: "active", value: true },
			{ op: "render", width: 20 },
			{ op: "active", value: false },
			{ op: "scrollbar", value: "always" },
			{ op: "render", width: 20 },
			{ op: "scrollbar", value: "hidden" },
		],
	},
	"scrollbar-always-width": {
		options: { scrollbar: "always" },
		lines: ["abcdefghijklmnopqrst", "short"],
		steps: [
			{ op: "layout", contentHeight: 2, viewportHeight: 5 },
			{ op: "render", width: 20 },
			{ op: "render", width: 1 },
			{ op: "layout", contentHeight: 2, viewportHeight: 0 },
		],
	},
	"overscroll-contain": {
		options: { overscroll: "contain", primary: true },
		lines: lines("line", 12),
		steps: [
			{ op: "layout", contentHeight: 12, viewportHeight: 5 },
			{ op: "scrollBy", lines: -3 },
			{ op: "scrollBy", lines: 200 },
			{ op: "render", width: 10 },
		],
	},
	"suppressed-follow-cleared-by-scroll": {
		options: { follow: "end" },
		lines: lines("line", 12),
		steps: [
			{ op: "layout", contentHeight: 12, viewportHeight: 5 },
			{ op: "scrollTo", top: 7, disableFollow: true },
			{ op: "scrollBy", lines: -3 },
			// Shrinking to one scroll line re-follows only when scrollBy cleared
			// the suppression.
			{ op: "layout", contentHeight: 6, viewportHeight: 5 },
		],
	},
	"empty-content": {
		options: { follow: "end", scrollbar: "auto" },
		lines: [],
		steps: [
			{ op: "layout", contentHeight: 0, viewportHeight: 5 },
			{ op: "scrollBy", lines: 5 },
			{ op: "end" },
			{ op: "start" },
		],
	},
};

const out = {};
for (const [label, spec] of Object.entries(cases)) {
	let renders = 0;
	const view = new ScrollView(new ScriptedComponent(spec.lines), spec.options);
	const record = (extra) => ({
		scrollTop: view.scrollTop,
		followingEnd: view.isFollowingEnd,
		viewportHeight: view.viewportHeight,
		scrollbar: view.scrollbar,
		scrollbarVisible: view.isScrollbarVisible,
		scrollbarActive: view.isScrollbarActive,
		renders,
		...extra,
	});
	const steps = [];
	for (const step of spec.steps) {
		let rendered;
		switch (step.op) {
			case "layout":
				view.updateLayout(step.contentHeight, step.viewportHeight, () => {
					renders++;
				});
				break;
			case "scrollTo":
				view.scrollTo(step.top, { disableFollow: step.disableFollow === true });
				break;
			case "scrollBy":
				rendered = view.scrollBy(step.lines);
				break;
			case "start":
				view.scrollToStart();
				break;
			case "end":
				view.scrollToEnd();
				break;
			case "scrollbar":
				view.setScrollbar(step.value);
				break;
			case "active":
				view.setScrollbarActive(step.value);
				break;
			case "render":
				rendered = view.render(step.width);
				break;
			default:
				throw new Error(`unknown op ${step.op}`);
		}
		steps.push(record(rendered === undefined ? {} : { result: rendered }));
	}
	out[label] = { spec, steps };
}
console.log(JSON.stringify(out, null, 2));
