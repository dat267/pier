// Regenerate from the repository root:
// node coding/testdata/remote-catalog/generate.mjs ./pi > coding/testdata/remote-catalog/golden.json
// Reference: pi v1.4.0 (4a080252), core/remote-catalog-provider.ts.
// Execute the pinned wrappers with stubbed HTTP, never an installed bundle.
import { registerHooks } from "node:module";
import { resolve } from "node:path";
import { pathToFileURL } from "node:url";

const root = resolve(process.argv[2]);
const sourceURL = (path) => pathToFileURL(resolve(root, path)).href;
// The checkout's dist files may be stale. Bridge only these package re-exports
// to their actual source implementations; do not replace catalog behavior.
const bridge = `
export {getModelType,isModelType} from ${JSON.stringify(sourceURL("packages/ai/src/utils/model-operations.ts"))};
export {formatUserAgent} from ${JSON.stringify(sourceURL("packages/ai/src/utils/identity.ts"))};`;
registerHooks({
 resolve(specifier, context, nextResolve) {
  if (specifier === "@earendil-works/pi-ai") {
   return { url: `data:text/javascript,${encodeURIComponent(bridge)}`, shortCircuit: true };
  }
  return nextResolve(specifier, context);
 },
});
const { withRemoteCatalog, withPersonalizedCatalog } = await import(sourceURL("packages/coding-agent/src/core/remote-catalog-provider.ts"));
const scenarios = [
 { name: "string-key-order", payload: '{"z":{"id":"z"},"a":{"id":"a"},"m":{"id":"m"}}' },
 { name: "index-key-order", payload: '{"10":{"id":"ten"},"z":{"id":"z"},"2":{"id":"two"},"01":{"id":"leading-zero"},"0":{"id":"zero"},"4294967295":{"id":"not-index"},"4294967294":{"id":"last-index"}}' },
 { name: "duplicate-key-order", payload: '{"z":{"id":"old"},"a":{"id":"a"},"z":{"id":"replacement"},"m":{"id":"m"}}' },
 { name: "models-array", payload: '{"models":[{"id":"z"},{"id":"a"},{"id":"m"}],"metadata":{"id":"ignored"}}' },
 { name: "models-object-fallback", payload: '{"z":{"id":"z"},"models":{"id":"models"},"a":{"id":"a"}}' },
 { name: "empty-object", payload: '{}' },
];
const fixtures = [];
for (const scenario of scenarios) {
 for (const personalized of [false, true]) {
  const base = { id: "p", getModels: () => [] };
  const provider = personalized
   ? withPersonalizedCatalog(base, async models => models, "https://catalog.invalid")
   : withRemoteCatalog(base, "https://catalog.invalid");
  let saved;
  globalThis.fetch = async () => new Response(scenario.payload, { status: 200 });
  await provider.refreshModels({
   allowNetwork: true, signal: new AbortController().signal,
   publish: async publication => {
    publication.update?.();
    if (publication.persist) saved = publication.persist;
    return true;
   },
  });
  fixtures.push({ ...scenario, personalized, persistedIds: saved.models.map(model => model.id), chatIds: provider.getModels().map(model => model.id) });
 }
}
console.log(JSON.stringify(fixtures, null, 2));
