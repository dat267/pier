// Regenerate from the repository root:
// node coding/testdata/models-store/generate.mjs ./pi > coding/testdata/models-store/golden.json
// Reference: pi v1.4.0 (4a080252), core/models-store.ts and api/openrouter-images.ts.
import { registerHooks } from "node:module";
import { resolve, join } from "node:path";
import { pathToFileURL } from "node:url";
import { mkdtempSync, writeFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";

const root = resolve(process.argv[2]);
const source = path => pathToFileURL(resolve(root, path)).href;
const bridge = `export {formatUserAgent} from ${JSON.stringify(source("packages/ai/src/utils/identity.ts"))};`;
registerHooks({ resolve(specifier, context, next) {
 if (specifier === "@earendil-works/pi-ai") return { url: `data:text/javascript,${encodeURIComponent(bridge)}`, shortCircuit: true };
 if (specifier === "@earendil-works/pi-telemetry") return { url: source("packages/telemetry/src/index.ts"), shortCircuit: true };
 return next(specifier, context);
}});
const { FileModelsStore } = await import(source("packages/coding-agent/src/core/models-store.ts"));
const { generateImages } = await import(source("packages/ai/src/api/openrouter-images.ts"));
const initial = JSON.stringify({
 openrouter: { models: [{
  type: "image", id: "image-model", output: ["image"], name: "Image",
  api: "openrouter-images", provider: "openrouter", baseUrl: "https://provider.invalid/v1",
  input: ["text"], vendor: { z: "<image>", a: 1 }, headers: { "X-Fixture": "present" },
  cost: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0 },
 }], checkedAt: 123 },
 other: { models: [], checkedAt: 123 },
});
const fixtures = [];
for (const operation of ["write-other", "delete-other", "update-metadata", "edit-model"]) {
 const dir = mkdtempSync(join(tmpdir(), "pier-models-store-fixture-"));
 try {
  const path = join(dir, "models-store.json");
  writeFileSync(path, initial);
  const store = new FileModelsStore(path);
  if (operation === "write-other") await store.write("other", { models: [], checkedAt: 456 });
  if (operation === "delete-other") await store.delete("other");
  if (operation === "update-metadata") {
   const stored = await store.read("openrouter");
   await store.write("openrouter", { ...stored, checkedAt: 456 });
  }
  if (operation === "edit-model") {
   const stored = await store.read("openrouter");
   stored.models[0].name = "Renamed";
   delete stored.models[0].headers;
   await store.write("openrouter", stored);
  }
  const stored = await store.read("openrouter");
  let requests = 0;
  globalThis.fetch = async () => {
   requests++;
   return new Response(JSON.stringify({ id: "r", choices: [{ message: {
    content: null, images: [{ image_url: { url: "data:image/png;base64,AA==" } }],
   } }] }), { status: 200, headers: { "content-type": "application/json" } });
  };
  const result = await generateImages(stored.models[0], { input: [{ type: "text", text: "draw a dot" }] }, { apiKey: "mock-key", env: {}, maxRetries: 0 });
  if (result.stopReason !== "stop" || requests !== 1) throw Error(result.errorMessage ?? "generation did not send HTTP");
  fixtures.push({ operation, initial, modelJSON: JSON.stringify(stored.models[0]), checkedAt: stored.checkedAt,
   generation: { stopReason: result.stopReason, outputTypes: result.output.map(block => block.type) } });
 } finally { rmSync(dir, { recursive: true, force: true }); }
}
console.log(JSON.stringify(fixtures, null, 2));
