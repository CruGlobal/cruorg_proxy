import json, sys, collections
F = ["status", "location", "upstream", "sni", "up_path", "up_host", "edge_key", "xfp", "xfh"]
def load(p): return {(r["tag"], r["method"], r["req"]): r for r in map(json.loads, open(p))}
a, b = load(sys.argv[1]), load(sys.argv[2])
diffs = collections.defaultdict(list)
for k, ra in a.items():
    rb = b.get(k)
    if rb is None: diffs["missing"].append(k); continue
    for f in F:
        if ra.get(f) != rb.get(f): diffs[f].append((k, ra.get(f), rb.get(f)))
print(f"{len(a)} requests compared")
for f, items in diffs.items():
    print(f"\n== {f}: {len(items)} differ")
    for k, x, y in items[:8]: print(f"  {k[0]:16} {k[2][:60]:60} old={str(x)[:50]!r} new={str(y)[:50]!r}")
if not diffs: print("no differences")
