#!/usr/bin/env python3
"""Convert upstream pi's generated image-model data into ai/image_catalog.json.

Upstream ground truth: pi/packages/ai/src/providers/data/*.json, the same
generator output the chat catalog uses (`npm run generate-models` in packages/ai,
or `node scripts/generate-models.ts --strict --data-only`). Both catalogs are
flattened by upstream's model-catalog.ts: every entry whose `type` is "image",
keyed by model id under its provider, carried verbatim. The separate
`generate-image-models` script the older pin needed no longer exists.

Usage:
    python3 scripts/gen_image_catalog.py /path/to/pi > ai/image_catalog.json
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

    providers = {}
    for file in sorted(data_dir.glob("*.json")):
        if file.name.startswith("."):
            continue  # .manifest.json is not a provider catalog
        for models in json.loads(file.read_text()).values():
            for model in models.values():
                if model.get("type") != "image":
                    continue
                providers.setdefault(model["provider"], {})[model["id"]] = model

    json.dump({"providers": providers}, sys.stdout, indent=1, sort_keys=True)
    sys.stdout.write("\n")


if __name__ == "__main__":
    main()
