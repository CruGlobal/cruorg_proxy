#!/usr/bin/env python3
"""Probe a live cruorg_proxy environment through CloudFront and record answers.

Usage:
  stage_probe.py <redirects.tf> <base-url> <out.jsonl> [--delay 0.8]
  stage_probe.py --compare <before.jsonl> <after.jsonl>

Run it against stage before and after an image change and compare the two
files. Redirects compare status and Location; proxied requests compare status
and the headers that show which backend answered (AEM or WordPress VIP).
Requests are sent one at a time with a delay so the WAF's rate rules don't trip.
"""
import collections
import http.client
import json
import sys
import time
import urllib.parse

sys.path.insert(0, __import__("os").path.dirname(__file__))

# WordPress VIP marks its responses with x-rq; anything else came from AEM.
# Server and Via are not compared: nginx stamped its own Server header, and Via
# carries a per-request CloudFront id.
VIP_HEADERS = ["x-rq", "x-hacker"]


def backend(r):
    return "vip" if any(r.getheader(h) for h in VIP_HEADERS) else "aem"


def parse_hashes(tf_path):
    import re
    hashes, key = {}, None
    line_re = re.compile(r'^\s*"((?:[^"\\]|\\.)*)"\s*=\s*"((?:[^"\\]|\\.)*)"')
    for ln in open(tf_path):
        m = re.match(r'\s*key\s*=\s*"([^"]+)"', ln)
        if m:
            key = m.group(1)
            hashes[key] = {}
            continue
        m = line_re.match(ln)
        if m and key:
            k, v = (json.loads('"%s"' % s) for s in m.groups())
            hashes[key][k] = v
    return hashes


def cases(hashes):
    out = []
    for k in hashes.get("cruorg:vanities", {}):
        out += [(k, "vanity"), (k.upper(), "vanity-upper"), (k + "?utm_source=probe", "vanity-query")]
    for pat in hashes.get("cruorg:upstreams", {}):
        p = pat.lstrip("^").replace(".*", "").replace("\\", "")
        if p.startswith("/") and "(" not in p:
            out.append((p + "/", "upstream"))
    out += [(p, "edge") for p in [
        "/", "/us/en.html", "/cru-nav.js", "/monitor.html", "//10steps", "/does-not-exist-probe",
        "/wp-json/", "/wp-admin/", "/campus/test", "/foo/index.html", "/us/en.html?utm_source=probe",
    ]]
    return out


CANARY = "/us/en.html"


def waf_blocked(r):
    return r.status == 403 and (r.getheader("x-cache") or "").startswith("Error from cloudfront")


def fetch(host, path):
    c = http.client.HTTPSConnection(host, timeout=30)
    c.request("GET", path, headers={"User-Agent": "cruorg-proxy-stage-probe"})
    r = c.getresponse()
    r.read()
    return r


def probe(tf_path, base, out_path, delay):
    u = urllib.parse.urlparse(base)
    rows = cases(parse_hashes(tf_path))
    with open(out_path, "w") as out:
        for i, (path, tag) in enumerate(rows):
            r, err, waf = None, None, False
            for attempt in range(4):
                try:
                    r = fetch(u.hostname, path)
                except OSError as e:
                    r, err = None, str(e)
                    time.sleep(2)
                    continue
                if not waf_blocked(r):
                    break
                # A WAF rule blocks this URL for everyone (scanner-looking
                # paths such as /cgi-bin/). That answer is the same before and
                # after an image change, so record it. If a known-good page is
                # blocked too, the rate rule has blocked this IP: wait it out.
                if not waf_blocked(fetch(u.hostname, CANARY)):
                    waf = True
                    break
                print(f"IP blocked by the WAF at request {i + 1}; waiting 330s", file=sys.stderr)
                time.sleep(330)
            rec = {"tag": tag, "req": path}
            if r is None:
                rec["error"] = err
            elif waf_blocked(r) and not waf:
                rec["error"] = "IP blocked by the CloudFront WAF"
            else:
                rec.update(status=r.status, location=r.getheader("Location"),
                           backend="waf" if waf else backend(r))
            out.write(json.dumps(rec) + "\n")
            if (i + 1) % 100 == 0:
                print(f"{i + 1}/{len(rows)}", file=sys.stderr)
            time.sleep(delay)
    print(f"{len(rows)} requests written to {out_path}")


def compare(a_path, b_path):
    load = lambda p: {(r["tag"], r["req"]): r for r in map(json.loads, open(p))}
    a, b = load(a_path), load(b_path)
    diffs = collections.defaultdict(list)
    for k, ra in a.items():
        rb = b.get(k, {})
        for f in ("status", "location", "error"):
            if ra.get(f) != rb.get(f):
                diffs[f].append((k, ra.get(f), rb.get(f)))
        if ra.get("status") != 301 and ra.get("backend") != rb.get("backend"):
            diffs["backend"].append((k, ra.get("backend"), rb.get("backend")))
    print(f"{len(a)} requests compared")
    for f, items in diffs.items():
        print(f"\n== {f}: {len(items)} differ")
        for k, x, y in items[:10]:
            print(f"  {k[0]:13} {k[1][:55]:55} before={str(x)[:60]!r} after={str(y)[:60]!r}")
    if not diffs:
        print("no differences")


if __name__ == "__main__":
    if sys.argv[1] == "--compare":
        compare(sys.argv[2], sys.argv[3])
    else:
        d = float(sys.argv[sys.argv.index("--delay") + 1]) if "--delay" in sys.argv else 0.8
        probe(sys.argv[1], sys.argv[2], sys.argv[3], d)
