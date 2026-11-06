// Reference: pi v1.5.0 (f9a26365), core/compaction/compaction.ts findCutPoint and
// prepareCompaction over the upstream v1 fixture before-compaction.jsonl, after
// migrateSessionEntries adds the v2 tree fields.
// Regenerate from the repository root:
// node --experimental-strip-types coding/testdata/upstream-compaction/generate.mjs ./pi > coding/testdata/upstream-compaction/golden.json
import { readFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";
const root = resolve(process.argv[2] ?? "./pi");
const here = dirname(fileURLToPath(import.meta.url));
const sm = await import(pathToFileURL(join(root, "packages/coding-agent/src/core/session-manager.ts")));
const comp = await import(pathToFileURL(join(root, "packages/coding-agent/src/core/compaction/index.ts")));
const entries = sm.parseSessionEntries(readFileSync(join(here, "before-compaction.jsonl"), "utf8"));
sm.migrateSessionEntries(entries);
const path = entries.filter(entry => entry.type !== "session");
const settings = comp.DEFAULT_COMPACTION_SETTINGS;
const cut = comp.findCutPoint(path, 0, path.length, settings.keepRecentTokens);
const prep = comp.prepareCompaction(path, settings);
console.log(JSON.stringify({
  fixture: "before-compaction.jsonl",
  settings: { enabled: settings.enabled, reserveTokens: settings.reserveTokens, keepRecentTokens: settings.keepRecentTokens },
  entries: path.length,
  cut: {
    index: cut.firstKeptEntryIndex,
    turnStartIndex: cut.turnStartIndex,
    isSplitTurn: cut.isSplitTurn,
    type: path[cut.firstKeptEntryIndex]?.type ?? null,
  },
  preparation: {
    firstKeptIndex: path.findIndex(entry => entry.id === prep.firstKeptEntryId),
    tokensBefore: prep.tokensBefore,
    isSplitTurn: prep.isSplitTurn,
    summarize: prep.messagesToSummarize.length,
    turnPrefix: prep.turnPrefixMessages.length,
    previousSummaryLength: prep.previousSummary ? prep.previousSummary.length : 0,
  },
}, null, 2));
