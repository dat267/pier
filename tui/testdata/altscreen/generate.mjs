// pi v1.5.2 (b4afdddf), packages/tui/src/tui-alt-screen.ts. Drives the
// reference TuiAltScreen with a fake recording terminal and scripted
// content/input steps, and prints, per case, the concatenated writes of every
// step plus the text its copySelection hook received. Read-only source load; run
// from the repository root:
//
//	node tui/testdata/altscreen/generate.mjs ./pi > tui/testdata/altscreen/golden.json
import { resolve } from "node:path";

const root = resolve(process.argv[2] ?? "./pi");
const { TuiAltScreen } = await import(new URL(`file://${root}/packages/tui/src/index.ts`).href);

// fakeTerminal mirrors tui/mainscreen_test.go's recordingTerminal.
class FakeTerminal {
	constructor(columns, rows) {
		this.width = columns;
		this.height = rows;
		this.writes = [];
		this.inputHandler = undefined;
	}
	start(onInput) {
		this.inputHandler = onInput;
	}
	stop() {
		this.inputHandler = undefined;
	}
	restoreOnExit() {}
	async drainInput() {}
	write(data) {
		this.writes.push(data);
	}
	get columns() {
		return this.width;
	}
	get rows() {
		return this.height;
	}
	get kittyProtocolActive() {
		return false;
	}
	moveBy() {}
	hideCursor() {
		this.writes.push("<HIDE>");
	}
	showCursor() {
		this.writes.push("<SHOW>");
	}
	clearLine() {}
	clearFromCursor() {}
	clearScreen() {}
	setTitle() {}
	setProgress() {}
	setProgramStatus() {}
}

// ScriptedComponent mirrors tui/mainscreen_test.go's scriptedComponent: it pads
// each line to the render width by rune count, so the write streams below are
// the ones pier's replay compares against.
// VerbatimComponent renders lines unchanged, like tui/fixedLinesComponent, so a
// case can hand the screen a line whose raw length exceeds the width without
// the padding truncating an escape sequence.
class VerbatimComponent {
	constructor() {
		this.lines = [];
	}
	render() {
		return this.lines;
	}
	invalidate() {}
}

class ScriptedComponent {
	constructor() {
		this.lines = [];
	}
	render(width) {
		return this.lines.map((line) => {
			const runes = Array.from(line);
			return runes.length >= width ? runes.slice(0, width).join("") : line + " ".repeat(width - runes.length);
		});
	}
	invalidate() {}
}

const cases = {
	"first-render": {"steps": [{"lines": ["a", "b"]}]},
	"content-change": {"steps": [{"lines": ["a", "b"]}, {"lines": ["a", "X"]}]},
	"scroll-keys": {"steps": [{"lines": ["line0", "line1", "line2", "line3", "line4", "line5", "line6", "line7", "line8", "line9", "line10", "line11", "line12", "line13"]}, {"inputs": ["\u001b[6~"]}, {"inputs": ["\u001b[5~"]}, {"inputs": ["\u001b[1;5H"]}, {"inputs": ["\u001b[1;5F"]}]},
	"wheel": {"steps": [{"lines": ["line0", "line1", "line2", "line3", "line4", "line5", "line6", "line7", "line8", "line9", "line10", "line11", "line12", "line13"]}, {"inputs": ["\u001b[<64;3;3M", "\u001b[<65;3;3M"]}]},
	"search": {"steps": [{"lines": ["alpha", "beta", "alpha"]}, {"inputs": ["\u001b[102;6u"]}, {"inputs": ["a"]}, {"inputs": ["l"]}, {"inputs": ["\r", "\r"]}, {"inputs": ["\u001b"]}]},
	"flash": {"steps": [{"lines": ["a"]}, {"flashes": ["Copied!"]}]},
	"indicator": {"steps": [{"lines": ["line0", "line1", "line2", "line3", "line4", "line5", "line6", "line7", "line8", "line9", "line10", "line11", "line12", "line13"]}], "indicator": true},
	"resize": {"steps": [{"lines": ["a", "b"]}, {"columns": 12, "rows": 3}]},
	"selection-drag": {"steps": [{"lines": ["hello world", "second line"]}, {"inputs": ["\u001b[<0;2;1M", "\u001b[<32;8;1M", "\u001b[<0;8;1m"]}]},
	"double-click": {"steps": [{"lines": ["hello world"]}, {"inputs": ["\u001b[<0;3;1M", "\u001b[<0;3;1m", "\u001b[<0;3;1M", "\u001b[<0;3;1m"]}]},
	"scrollbar-drag": {"steps": [{"lines": ["line0", "line1", "line2", "line3", "line4", "line5", "line6", "line7", "line8", "line9", "line10", "line11", "line12", "line13"]}, {"inputs": ["\u001b[<0;24;3M", "\u001b[<32;24;4M", "\u001b[<0;24;4m"]}]},
	"focus-out": {"steps": [{"lines": ["a"]}, {"inputs": ["\u001b[I", "\u001b[O"]}]},
	"stop": {"steps": [{"lines": ["a", "b"]}], "stop": true},
	"prompt-jump": {"steps": [{"lines": ["\u001b]133;A\u0007prompt1", "body", "\u001b]133;A\u0007prompt2", "body2", "tail", "x", "y", "z", "w"], "rows": 3}, {"inputs": ["\u001b[1;6A", "\u001b[1;6A"]}]},
	"copy-drag-mid": {"steps": [{"lines": ["hello world", "second line"]}, {"inputs": ["\u001b[<0;2;1M", "\u001b[<32;8;1M", "\u001b[<0;8;1m"]}], "copySelection": true},
	"copy-drag-multiline": {"steps": [{"lines": ["hello world", "second line"]}, {"inputs": ["\u001b[<0;4;1M", "\u001b[<32;3;2M", "\u001b[<0;3;2m"]}], "copySelection": true},
	"copy-drag-backwards": {"steps": [{"lines": ["hello world", "second line"]}, {"inputs": ["\u001b[<0;9;2M", "\u001b[<32;2;1M", "\u001b[<0;2;1m"]}], "copySelection": true},
	"copy-double-click": {"steps": [{"lines": ["hello world"]}, {"inputs": ["\u001b[<0;3;1M", "\u001b[<0;3;1m", "\u001b[<0;3;1M", "\u001b[<0;3;1m"]}], "copySelection": true},
	"copy-triple-click": {"steps": [{"lines": ["hello world", "tail"]}, {"inputs": ["\u001b[<0;3;1M", "\u001b[<0;3;1m", "\u001b[<0;3;1M", "\u001b[<0;3;1m", "\u001b[<0;3;1M", "\u001b[<0;3;1m"]}], "copySelection": true},
	"copy-wide-chars": {"steps": [{"lines": ["\u65e5\u672c\u8a9e", "ascii tail"]}, {"inputs": ["\u001b[<0;1;1M", "\u001b[<32;7;1M", "\u001b[<0;7;1m"]}], "copySelection": true, "verbatim": true},
	"copy-padded-line": {"steps": [{"lines": ["ab   ", "cd"]}, {"inputs": ["\u001b[<0;1;1M", "\u001b[<32;24;1M", "\u001b[<0;24;1m"]}], "copySelection": true},
	"wheel-alt": {
		steps: [
			{ lines: ["line0", "line1", "line2", "line3", "line4", "line5", "line6", "line7", "line8", "line9", "line10", "line11", "line12", "line13"] },
			{ inputs: ["\u001b[<72;3;3M"] },
			{ inputs: ["\u001b[<72;3;3M"] },
		],
	},
	"search-nav": {
		steps: [
			{ lines: ["alpha", "beta", "alpha", "gamma", "alpha"] },
			{ inputs: ["\u001b[102;6u", "a"] },
			{ inputs: ["\u001b[1;2B"] },
			{ inputs: ["\u001b[1;2B"] },
			{ inputs: ["\u001b[1;2A"] },
			{ inputs: ["\r"] },
		],
	},
	"selection-word-drag": {
		steps: [
			{ lines: ["hello world again", "tail"] },
			{ inputs: ["\u001b[<0;3;1M", "\u001b[<0;3;1m", "\u001b[<0;3;1M", "\u001b[<32;15;1M", "\u001b[<0;15;1m"] },
		],
		copySelection: true,
	},
	"selection-line-drag": {
		steps: [
			{ lines: ["hello world", "second line", "third"] },
			{ inputs: ["\u001b[<0;3;1M", "\u001b[<0;3;1m", "\u001b[<0;3;1M", "\u001b[<0;3;1m", "\u001b[<0;3;1M", "\u001b[<32;4;2M", "\u001b[<0;4;2m"] },
		],
		copySelection: true,
	},
	"selection-tabs-emoji": {
		// Verbatim: padding by rune count would widen a tab or emoji line past
		// the terminal, and the clamp then drops upstream's reset (D470).
		verbatim: true,
		steps: [
			{ lines: ["a\tb\tc", "\ud83d\ude00 emoji \ud83c\udf89"] },
			{ inputs: ["\u001b[<0;1;1M", "\u001b[<32;20;2M", "\u001b[<0;20;2m"] },
		],
		copySelection: true,
	},
	"url-click": {
		// The press reads the URL from the last drawn frame, so the link must be
		// on screen before the click. The raw line is longer than the width, so
		// the component must not pad it (padding would cut the escape).
		verbatim: true,
		steps: [
			{ lines: ["see \u001b]8;;https://example.com/x\u001b\\the link\u001b]8;;\u001b\\ for more"] },
			{ inputs: ["\u001b[<0;6;1M"] },
			{ inputs: ["\u001b[<0;6;1m"] },
		],
	},
	"right-click": {
		steps: [{ lines: ["hello"] }, { inputs: ["\u001b[<2;3;1M", "\u001b[<2;3;1m"] }],
	},
	"resize-during-selection": {
		steps: [
			{ lines: ["hello world", "second line"] },
			{ inputs: ["\u001b[<0;2;1M", "\u001b[<32;8;1M"] },
			{ columns: 12, rows: 3 },
			{ inputs: ["\u001b[<0;8;1m"] },
		],
		copySelection: true,
	},
	"copy-no-drag": {"steps": [{"lines": ["hello world"]}, {"inputs": ["\u001b[<0;3;1M", "\u001b[<0;3;1m"]}], "copySelection": true},
};


const out = {};
for (const [label, spec] of Object.entries(cases)) {
	const terminal = new FakeTerminal(24, 6);
	const options = {};
	if (spec.indicator) options.scrollToEndIndicator = () => " ↓ Jump to latest message ";
	const copied = [];
	const urls = [];
	const pastes = [];
	if (spec.copySelection) {
		options.copySelection = async (text) => {
			copied.push(text);
			return true;
		};
	}
	options.openUrl = (url) => urls.push(url);
	options.onRightClickPaste = () => pastes.push("paste");
	const screen = new TuiAltScreen(terminal, false, undefined, options);
	const component = spec.verbatim ? new VerbatimComponent() : new ScriptedComponent();
	screen.addChild(component);
	const inputHandler = (data) => terminal.inputHandler(data);

	// Copy-on-select runs the hook off the input path, so let its microtask run
	// before reading the text that step produced.
	// Yield microtasks only: the reference's input path schedules a 16ms render
	// timer, and letting it fire would draw the frame before this harness does.
	const settle = async () => {
		for (let tick = 0; tick < 5; tick++) await Promise.resolve();
	};

	terminal.writes = [];
	screen.start();
	const steps = [terminal.writes.join("")];
	const copies = [[]];
	const urlSteps = [[]];
	const pasteSteps = [[]];
	await settle();
	copies[0] = copied.splice(0);
	urlSteps[0] = urls.splice(0);
	pasteSteps[0] = pastes.splice(0);
	for (const step of spec.steps) {
		if (step.lines) component.lines = step.lines;
		if (step.columns !== undefined) terminal.width = step.columns;
		if (step.rows !== undefined) terminal.height = step.rows;
		for (const input of step.inputs ?? []) inputHandler(input);
		for (const flash of step.flashes ?? []) screen.flash(flash);
		// Let a copy-on-select hook finish before the frame is drawn, so the
		// "Copied!" flash lands in this step on both sides instead of racing the
		// manual render.
		await settle();
		copies.push(copied.splice(0));
		urlSteps.push(urls.splice(0));
		pasteSteps.push(pastes.splice(0));
		terminal.writes = [];
		screen.renderNow(false);
		steps.push(terminal.writes.join(""));
	}
	if (spec.stop) {
		terminal.writes = [];
		screen.stop();
		steps.push(terminal.writes.join(""));
		await settle();
		copies.push(copied.splice(0));
		urlSteps.push(urls.splice(0));
		pasteSteps.push(pastes.splice(0));
	}
	out[label] = { spec, steps, copied: copies, urls: urlSteps, pastes: pasteSteps };
}
console.log(JSON.stringify(out, null, 2));
