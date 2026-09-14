#!/usr/bin/env python3
"""Refresh the public Trello REST schema; never reads account credentials."""
import hashlib
import json
from pathlib import Path
from urllib.request import urlopen

SOURCE = "https://dac-static.atlassian.com/cloud/trello/swagger.v3.json"
ROOT = Path(__file__).resolve().parents[1]

def clean(value):
    if isinstance(value, dict):
        return {k: clean(v) for k, v in value.items() if k not in {"example", "examples"}}
    if isinstance(value, list):
        return [clean(v) for v in value]
    return value

with urlopen(SOURCE, timeout=60) as response:
    raw = response.read()
spec = json.loads(raw)
catalog = clean(spec)
catalog["x-source-url"] = SOURCE
catalog["x-source-sha256"] = hashlib.sha256(raw).hexdigest()
target = ROOT / "cli" / "trello-openapi.json"
target.write_text(json.dumps(catalog, ensure_ascii=False, indent=2) + "\n")
operations = sum(method in {"get", "post", "put", "delete", "patch", "head", "options"}
                 for path in spec["paths"].values() for method in path)
print(f"Saved {len(spec['paths'])} paths, {operations} operations to {target.name}")
