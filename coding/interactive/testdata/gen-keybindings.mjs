// Reference: pi v1.6.0 (faac5858) coding-agent/src/core/keybindings.ts
// KEYBINDINGS, on the linux non-WSL platform the harness runs with (the
// reference computes the Windows and WSL variants at module load from
// process.platform, which a golden cannot vary).
// node --experimental-strip-types coding/interactive/testdata/gen-keybindings.mjs ./pi
import { upstream } from "./golden-reference.mjs";
const mod = await upstream("coding-agent/src/core/keybindings.ts");
for (const [key, def] of Object.entries(mod.KEYBINDINGS)) {
	const keys = Array.isArray(def.defaultKeys) ? def.defaultKeys : def.defaultKeys === undefined ? [] : [def.defaultKeys];
	console.log(`keybind ${key} => ${JSON.stringify({ keys, description: def.description ?? "" })}`);
}
