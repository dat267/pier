// Reference: pi v1.5.0 (f9a26365), core/compaction/compaction.ts findCutPoint and
// prepareCompaction over the upstream v1 fixture before-compaction.jsonl, after
// migrateSessionEntries adds the v2 tree fields.
// Regenerate from the repository root:
// node --experimental-strip-types coding/testdata/upstream-compaction/generate.mjs ./pi > coding/testdata/upstream-compaction/golden.json
import { createHash } from "node:crypto";
import { readFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";
const root = resolve(process.argv[2] ?? "./pi");
const here = dirname(fileURLToPath(import.meta.url));
const sm = await import(pathToFileURL(join(root, "packages/coding-agent/src/core/session-manager.ts")));
const comp = await import(pathToFileURL(join(root, "packages/coding-agent/src/core/compaction/index.ts")));
const utils = await import(pathToFileURL(join(root, "packages/coding-agent/src/core/compaction/utils.ts")));
const messagesMod = await import(pathToFileURL(join(root, "packages/coding-agent/src/core/messages.ts")));
const entries = sm.parseSessionEntries(readFileSync(join(here, "before-compaction.jsonl"), "utf8"));
sm.migrateSessionEntries(entries);
const path = entries.filter(entry => entry.type !== "session");
const settings = comp.DEFAULT_COMPACTION_SETTINGS;
const cut = comp.findCutPoint(path, 0, path.length, settings.keepRecentTokens);
const context = sm.buildSessionContext(path);
const describeBlock = block => {
  if (block.type === "text") return `text:${block.text.length}`;
  if (block.type === "thinking") return `thinking:${block.thinking.length}`;
  if (block.type === "toolCall") return `toolCall:${block.name}:${JSON.stringify(block.arguments ?? {}).length}`;
  if (block.type === "image") return "image";
  return block.type;
};
const lines = context.messages.map(message => {
  const content = message.content;
  let blocks;
  if (typeof content === "string") blocks = [`text:${content.length}`];
  else if (Array.isArray(content)) blocks = content.map(describeBlock);
  else blocks = [];
  return `${message.role}|${blocks.join(",")}`;
});
const projectionDigest = createHash("sha256").update(lines.join("\n")).digest("hex");
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
  projection: { count: context.messages.length, digest: projectionDigest },
  conversation: (() => {
    const text = utils.serializeConversation(messagesMod.convertToLlm(context.messages));
    return { length: text.length, digest: createHash("sha256").update(text).digest("hex") };
  })(),
  preparation: {
    firstKeptIndex: path.findIndex(entry => entry.id === prep.firstKeptEntryId),
    tokensBefore: prep.tokensBefore,
    isSplitTurn: prep.isSplitTurn,
    summarize: prep.messagesToSummarize.length,
    turnPrefix: prep.turnPrefixMessages.length,
    previousSummaryLength: prep.previousSummary ? prep.previousSummary.length : 0,
  },
}, null, 2));
