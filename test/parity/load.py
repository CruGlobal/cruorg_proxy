import json, re, sys
src = open(sys.argv[1]).read().splitlines()
hashes, cur, key = {}, None, None
line_re = re.compile(r'^\s*"((?:[^"\\]|\\.)*)"\s*=\s*"((?:[^"\\]|\\.)*)"')
for ln in src:
    m = re.match(r'\s*resource\s+"redisdb_hash"\s+"(\w+)"', ln)
    if m: cur = m.group(1); continue
    m = re.match(r'\s*key\s*=\s*"([^"]+)"', ln)
    if m and cur: key = m.group(1); hashes[key] = {}; continue
    m = line_re.match(ln)
    if m and key:
        k, v = (json.loads('"%s"' % s) for s in m.groups())
        hashes[key][k] = v
json.dump(hashes, open("hashes.json", "w"), indent=1)
rules = {
    "vanities": hashes.get("cruorg:vanities", {}),
    "rewrites": hashes.get("cruorg:regex", {}),
    "upstreams": hashes.get("cruorg:upstreams", {}),
    "forward_query": sorted(hashes.get("cruorg:forward_query", {})),
}
json.dump(rules, open("rules.json", "w"), indent=1)
out = sys.stdout.buffer
def cmd(*a):
    out.write(b"*%d\r\n" % len(a))
    for x in a:
        b = x.encode(); out.write(b"$%d\r\n%s\r\n" % (len(b), b))
for k, h in hashes.items():
    cmd("DEL", k)
    for f, v in h.items(): cmd("HSET", k, f, v)
    print({k: len(h)}, file=sys.stderr)
