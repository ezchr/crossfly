#!/usr/bin/env python3
"""Bundle the vanilla 26.3 villager trade data (villager_trade, trade_set,
their tags, the enchantments trades use and the potion tag) into one JSON
file for server/mob/jvillage. Usage: gen_trades.py <builtin data/minecraft> <out.json>"""
import json, os, sys

root, out = sys.argv[1], sys.argv[2]

def load(p):
    with open(p) as f:
        return json.load(f)

def strip(n):
    return n[len("minecraft:"):] if n.startswith("minecraft:") else n

def expand(kind, ref, seen=None):
    """Expand a tag reference ("#minecraft:x") or plain id into ordered ids."""
    if isinstance(ref, list):
        res = []
        for r in ref:
            for x in expand(kind, r, seen):
                if x not in res:
                    res.append(x)
        return res
    if not ref.startswith("#"):
        return [strip(ref)]
    path = os.path.join(root, "tags", kind, strip(ref[1:]) + ".json")
    res = []
    for v in load(path)["values"]:
        if isinstance(v, dict):
            v = v["id"]
        for x in expand(kind, v):
            if x not in res:
                res.append(x)
    return res

data = {"trades": {}, "trade_tags": {}, "trade_sets": {}, "enchantments": {}, "enchantment_tags": {}, "potion_tags": {}}
tr = os.path.join(root, "villager_trade")
for d, _, files in os.walk(tr):
    for f in files:
        if f.endswith(".json"):
            key = os.path.relpath(os.path.join(d, f), tr)[:-5]
            data["trades"][key] = load(os.path.join(d, f))
tt = os.path.join(root, "tags", "villager_trade")
for d, _, files in os.walk(tt):
    for f in files:
        key = os.path.relpath(os.path.join(d, f), tt)[:-5]
        data["trade_tags"][key] = expand("villager_trade", "#minecraft:" + key)
ts = os.path.join(root, "trade_set")
for d, _, files in os.walk(ts):
    for f in files:
        key = os.path.relpath(os.path.join(d, f), ts)[:-5]
        data["trade_sets"][key] = load(os.path.join(d, f))

for f in sorted(os.listdir(os.path.join(root, "enchantment"))):
    e = load(os.path.join(root, "enchantment", f))
    name = f[:-5]
    data["enchantments"][name] = {
        "weight": e["weight"],
        "max_level": e["max_level"],
        "min_cost": [e["min_cost"]["base"], e["min_cost"]["per_level_above_first"]],
        "max_cost": [e["max_cost"]["base"], e["max_cost"]["per_level_above_first"]],
        "supported": expand("item", e["supported_items"]),
        "primary": expand("item", e["primary_items"]) if "primary_items" in e else None,
        "exclusive": expand("enchantment", e["exclusive_set"]) if "exclusive_set" in e else [],
    }
for t in ("tradeable", "on_traded_equipment", "double_trade_price"):
    data["enchantment_tags"][t] = expand("enchantment", "#minecraft:" + t)
data["potion_tags"]["tradeable"] = expand("potion", "#minecraft:tradeable")

with open(out, "w") as f:
    json.dump(data, f, sort_keys=True, separators=(",", ":"))
print(len(data["trades"]), "trades", len(data["trade_sets"]), "sets", len(data["enchantments"]), "enchantments")
