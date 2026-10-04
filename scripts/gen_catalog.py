#!/usr/bin/env python3
"""Convert upstream pi's generated model data into ai/models_catalog.json.

Upstream ground truth: pi/packages/ai/src/providers/data/*.json (the provider
catalogs) + .manifest.json, produced by upstream's own generator:

    cd <upstream clone>/packages/ai && node scripts/generate-models.ts --strict --data-only

Usage:
    python3 scripts/gen_catalog.py /path/to/pi > ai/models_catalog.json

The output mirrors upstream's models.generated.ts MODELS shape:
    {provider: {api: {modelId: model}}}  plus a _meta block carrying the
    manifest's generatedAt timestamp (upstream getBuiltinModelDataGeneratedAt).

Schema 6 groups each upstream id under chat/image/classifier roles, keyed
"<role>:<id>". This catalog is the chat catalog, so only chat entries are kept
and the role prefix is stripped, matching upstream flattenChatModelCatalog;
providers with no chat entry are dropped. Schema 3 entries carry no role and
pass through unchanged.
"""
import json
import sys
from pathlib import Path


def main() -> None:
    if len(sys.argv) != 2:
        print(__doc__, file=sys.stderr)
        sys.exit(2)
    data_dir = Path(sys.argv[1]) / "packages" / "ai" / "src" / "providers" / "data"
    if not data_dir.is_dir():
        print(f"error: {data_dir} not found — run upstream's generate-models first", file=sys.stderr)
        sys.exit(1)

    manifest = json.loads((data_dir / ".manifest.json").read_text())
    providers = {}
    for file in sorted(data_dir.glob("*.json")):
        if file.name.startswith("."):
            continue  # .manifest.json is not a provider catalog
        # provider id = file stem (matches upstream models.generated.ts keys)
        raw = json.loads(file.read_text())
        # Schema 6 groups one upstream id under chat/image/classifier roles, keyed
        # "<role>:<id>". models_catalog.json is the chat catalog, so keep the
        # chat entries and strip the role prefix (upstream flattenChatModelCatalog).
        # Schema 3 entries carry no role and are taken verbatim.
        by_api = {}
        for api, entries in raw.items():
            chat = {}
            for identity, model in entries.items():
                role = model.get("type")
                if role is not None and role != "chat":
                    continue
                model_id = identity
                if role is not None and identity.startswith(role + ":"):
                    model_id = identity[len(role) + 1:]
                chat[model_id] = model
            if chat:
                by_api[api] = chat
        # Upstream generate-models.ts maps Cloudflare AI Gateway's anthropic
        # passthrough models with dotted versions (claude-opus-5.5) onto dashed
        # IDs (claude-opus-5-5): the gateway forwards the ID to Anthropic
        # unchanged, which rejects the dots (c10bfb0d7).
        if file.stem == "cloudflare-ai-gateway":
            anthropic = by_api.get("anthropic-messages")
            if anthropic:
                renamed = {}
                for model_id, model in anthropic.items():
                    dashed = model_id.replace(".", "-")
                    model = dict(model)
                    model["id"] = dashed
                    renamed[dashed] = model
                by_api["anthropic-messages"] = renamed
        if by_api:
            providers[file.stem] = by_api

    out = {
        "_meta": {
            "schemaVersion": manifest["schemaVersion"],
            "generatedAt": manifest["generatedAt"],
            "upstreamManifestSHA256": manifest.get("structureHash"),
        },
        "providers": providers,
    }
    json.dump(out, sys.stdout, indent=1, sort_keys=True)
    sys.stdout.write("\n")


if __name__ == "__main__":
    main()
