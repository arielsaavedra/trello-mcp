#!/usr/bin/env python3
"""Read-only MCP smoke test using existing local Codex config (Python 3.11+).

Prints counts/capabilities only, never credentials, names, descriptions or comments.
Writes no Trello data. Board choice comes exclusively from the saved allowlist.
"""
import argparse
import json
import os
from pathlib import Path
import select
import subprocess
import time
import tomllib

parser = argparse.ArgumentParser()
parser.add_argument("--config", type=Path, default=Path(os.environ.get("CODEX_HOME",str(Path.home()/".codex")))/"config.toml")
parser.add_argument("--server", default="mcp-trello")
parser.add_argument("--binary", type=Path)
parser.add_argument("--card-id", help="Optional card id/shortlink on the allowed board")
args=parser.parse_args()
entry=tomllib.loads(args.config.read_text())["mcp_servers"][args.server]
env={**os.environ,**entry.get("env",{})}
command=[str(args.binary)] if args.binary else [entry["command"],*entry.get("args",[])]
process=subprocess.Popen(command,env=env,stdin=subprocess.PIPE,stdout=subprocess.PIPE,stderr=subprocess.DEVNULL)
pending=b""
seq=0

def rpc(method,params):
    global seq,pending
    seq+=1
    process.stdin.write((json.dumps({"jsonrpc":"2.0","id":seq,"method":method,"params":params})+"\n").encode());process.stdin.flush()
    deadline=time.monotonic()+40
    while time.monotonic()<deadline:
        if b"\n" not in pending:
            if not select.select([process.stdout],[],[],max(0,deadline-time.monotonic()))[0]:break
            chunk=os.read(process.stdout.fileno(),65536)
            if not chunk:raise RuntimeError("Server exited")
            pending+=chunk
        while b"\n" in pending:
            line,pending=pending.split(b"\n",1)
            response=json.loads(line)
            if response.get("id")==seq:return response
    raise RuntimeError("MCP timeout")

def call(name,arguments):
    response=rpc("tools/call",{"name":name,"arguments":arguments})
    if response.get("error") or response.get("result",{}).get("isError"):
        raise RuntimeError(f"{name} failed; inspect a redacted error separately")
    result=response["result"]
    for item in result.get("content",[]):
        if item.get("type")=="text":return json.loads(item["text"])
    raise RuntimeError(f"{name}: unexpected response")

try:
    init=rpc("initialize",{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"read-only-smoke","version":"1"}})
    print("Server:",init["result"]["serverInfo"])
    process.stdin.write(b'{"jsonrpc":"2.0","method":"notifications/initialized"}\n');process.stdin.flush()
    tools=rpc("tools/list",{})["result"]["tools"]
    names={t["name"] for t in tools}
    required={"list_card_comments","list_card_history","list_board_members","copy_template_card","search_api_endpoints","get_api_endpoint","trello_api_read","trello_api_write"}
    assert required<=names,"Missing fork capabilities"
    print("Tools:",len(names),"required capabilities present")
    setup=call("get_setup_status",{})
    assert not setup["onboarding_required"]
    assert setup.get("api_scope")=="boards","Smoke test requires board scope"
    allowed=set(setup["allowed_board_ids"])
    boards=call("list_boards",{})
    assert {b["id"] for b in boards}==allowed
    print("Scope: boards; allowed boards:",len(allowed))
    board=boards[0]["id"]
    lists=call("list_lists",{"board_id":board})
    members=call("list_board_members",{"board_id":board})
    cards=call("list_cards",{"board_id":board,"include_closed":True})
    print("Read lists/members/cards:",len(lists),len(members),len(cards))
    chosen=args.card_id or next(c["id"] for c in cards if not c["closed"] and not c.get("isTemplate"))
    card=call("get_card",{"card_id":chosen})
    assert card["idBoard"]==board
    print("Card detail/checklists verified:",len(card.get("checklists",[])))
    for name in ("list_card_comments","list_card_history"):
        first=call(name,{"card_id":chosen,"limit":1})
        count=len(first["actions"])
        if first["has_more"]:
            second=call(name,{"card_id":chosen,"limit":1,"before":first["next_before"]})
            assert not ({a["id"] for a in first["actions"]}&{a["id"] for a in second["actions"]})
            count+=len(second["actions"])
        print(name,": read",count,"actions; paging verified" if first["has_more"] else ": final page")
    catalog=call("search_api_endpoints",{"keyword":"","limit":1})
    print("Official catalog operations:",catalog["total"])
    schema=call("get_api_endpoint",{"endpoint":"/boards/{id}","method":"GET"})
    assert schema["operation"]
    generic=call("trello_api_read",{"endpoint":"/boards/{id}","path_params":{"id":board},"query":{"fields":"name"}})
    assert generic["status"]==200 and generic["data"]["id"]==board
    print("Generic REST read: verified. Trello writes performed: 0.")
finally:
    process.terminate()
    try:process.wait(timeout=5)
    except subprocess.TimeoutExpired:process.kill();process.wait()
