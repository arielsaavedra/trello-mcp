#!/usr/bin/env python3
"""Point an existing Codex MCP entry at this build, preserving credentials/settings.

Requires Python 3.11+. Creates a protected rollback copy next to Codex config.
No tokens are printed or passed as command-line arguments.
"""
import argparse
from datetime import datetime, timezone
import json
import os
from pathlib import Path
import re
import tomllib

parser = argparse.ArgumentParser()
parser.add_argument("--config", type=Path, default=Path(os.environ.get("CODEX_HOME", str(Path.home()/".codex")))/"config.toml")
parser.add_argument("--server", default="mcp-trello")
args = parser.parse_args()
binary = Path(__file__).resolve().parents[1]/"dist"/"trello-mcp"
if not binary.is_file():
    raise SystemExit("Build the connector first with sh scripts/build-local.sh")
raw = args.config.read_text()
data = tomllib.loads(raw)
entry = data.get("mcp_servers", {}).get(args.server)
if not entry:
    raise SystemExit("Register this server in Codex with local credentials first; see docs/SETUP.md")
header = re.compile(r"(?m)^\[mcp_servers\." + re.escape(args.server) + r"\]\s*$")
match = header.search(raw)
if not match:
    raise SystemExit("Server table uses an unsupported layout; no changes made")
next_header = re.search(r"(?m)^\[", raw[match.end():])
end = match.end() + next_header.start() if next_header else len(raw)
section = raw[match.end():end]
# Current Codex-generated command/args are single-line TOML values. Fail rather
# than accidentally consume other tables when encountering a custom layout.
for key in ("command", "args"):
    lines = re.findall(r"(?m)^"+key+r"\s*=.*$", section)
    if key in entry and len(lines) != 1:
        raise SystemExit(f"Unsupported {key} layout; no changes made")
    for line in lines:
        try:
            tomllib.loads(line)
        except tomllib.TOMLDecodeError:
            raise SystemExit(f"Multiline {key} layout; no changes made")
    section = re.sub(r"(?m)^"+key+r"\s*=.*\n?", "", section)
section = '\ncommand = ' + json.dumps(str(binary)) + '\nargs = []\n' + section.lstrip('\n')
updated = raw[:match.end()] + section + raw[end:]
parsed = tomllib.loads(updated)
expected = dict(entry, command=str(binary), args=[])
if parsed["mcp_servers"][args.server] != expected:
    raise SystemExit("Verification failed; no changes made")
if updated == raw:
    print("Already configured; no changes needed")
else:
    stamp = datetime.now(timezone.utc).strftime("%Y%m%dT%H%M%SZ")
    backup = args.config.with_name(f"config.toml.before-trello-fork-{stamp}.bak")
    fd = os.open(backup, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    with os.fdopen(fd, "w") as handle:
        handle.write(raw)
    args.config.write_text(updated)
    args.config.chmod(0o600)
    print(f"Configured {args.server} to use {binary}")
    print(f"Protected rollback copy: {backup.name}")
