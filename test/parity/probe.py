import http.client, json, sys
port, out = int(sys.argv[1]), open(sys.argv[2], "w")
h = json.load(open("hashes.json"))
cases = []
def add(path, tag, method="GET", body=None): cases.append((method, path, tag, body))
for k in h["cruorg:vanities"]:
    add(k, "vanity"); add(k.upper(), "vanity-upper")
    if not k.endswith("/"): add(k + "/", "vanity-slash")
    add(k + "?utm_source=test&x=1", "vanity-query")
regex_samples = ["/foo/bar.htm", "/foo/index.html", "/campus/abc/def", "/City/Test", "/digitalministry/training/201/lesson1",
  "/highschool/x", "/highschoolstaff", "/member/123.html", "/military/abc", "/ministries-and-locations/x",
  "/ministries-and-locations/ministries/x", "/ministries-and-locations/ministries/athletes-in-action/x",
  "/ministries-and-locations/ministries/familylife/x", "/montana/x", "/train-and-grow/classics/10-basic-steps/1",
  "/train-and-grow/classics/transferable-concepts/1", "/train-and-grow/devotional-life/35-day-challenge-day/5",
  "/train-and-grow/devotional-life/7-steps-to-fasting.html", "/train-and-grow/devotional-life/discover-god/x",
  "/train-and-grow/devotional-life/personal-guide-to-fasting.html", "/train-and-grow/devotional-life/todays-promise.html",
  "/training-and-growth/x", "/us/en/communities/innercity/chicago.html", "/wc/2020", "/winterconference/2020",
  "/winterconferencedev/2020", "/storylines/x", "/communities/city/anything", "/communities/city-reflectingjesus/x",
  "/communities/city-engageandequip/x", "/communities/city-orangecounty/x", "/communities/city-missionshiftpodcast/x",
  "/CAMPUS/Mixed/Case", "/foo/bar.htm?a=1"]
for p in regex_samples: add(p, "regex")
for pat in h["cruorg:upstreams"]:
    p = pat.lstrip("^").replace(".*", "").replace("\\", "")
    if p.startswith("/") and "(" not in p: add(p + "/page", "upstream"); add(p.upper() + "/page", "upstream-upper")
for p in ["/wp-admin/", "/wp-content/x.css", "/xmlrpc.php", "/foo/bar.php?x=1", "/_static/x.js", "/.wpvip/x",
          "/sso/login", "/.well-known/acme-challenge/abc", "/us/en/train-and-grow/courses/x"]: add(p, "upstream-special")
for p in ["/", "/us/en.html", "/cru-nav.js", "/cru-nav.json", "/monitor.html", "/does-not-exist", "//10steps",
          "/10steps%20", "/%31%30steps", "/a//b/../c", "/10steps?purge_vanity=1", "/communities/x?purge_target=1",
          "/us/en.html?utm_source=a&b=c"]: add(p, "edge")
add("/wp-admin/admin-ajax.php", "post-vip", "POST", b"a=1"); add("/us/en/form.html", "post-aem", "POST", b"a=1")
for method, path, tag, body in cases:
    c = http.client.HTTPConnection("localhost", port, timeout=10)
    hdr = {"Host": "www.cru.org", "X-Forwarded-For": "203.0.113.9", "X-Forwarded-Proto": "https", "User-Agent": "harness"}
    c.request(method, path, body=body, headers=hdr); r = c.getresponse(); raw = r.read()
    rec = {"tag": tag, "method": method, "req": path, "status": r.status, "location": r.getheader("Location")}
    if r.status == 200 and r.getheader("Content-Type", "").startswith("application/json"):
        e = json.loads(raw); hh = e["headers"]
        rec.update(upstream=e["upstream"], sni=e["sni"], up_path=e["path"], up_host=hh.get("host"),
                   edge_key=hh.get("x-aem-edge-key"), xff=hh.get("x-forwarded-for"), xfp=hh.get("x-forwarded-proto"),
                   xfh=hh.get("x-forwarded-host"), xri=hh.get("x-real-ip"))
    else:
        rec["body_head"] = raw[:80].decode("latin1")
    out.write(json.dumps(rec) + "\n")
print(len(cases), "cases")
