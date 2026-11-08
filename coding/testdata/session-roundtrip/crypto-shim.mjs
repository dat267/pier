// Deterministic stand-in for node:crypto inside the golden generator.
// The pinned SessionManager draws every entry id from randomUUID(), so the
// fixture would otherwise change on every run. The generator redirects
// "node:crypto" here; the re-export below resolves through the real builtin
// because the hook skips this file.
export * from "node:crypto";

let counter = 0;

export function randomUUID() {
	counter += 1;
	return `00000000-0000-4000-8000-${counter.toString(16).padStart(12, "0")}`;
}
