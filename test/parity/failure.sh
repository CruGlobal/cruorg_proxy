#!/usr/bin/env bash
# Failure-mode and feature checks against the Go build (port 18081). Run after
# run.sh. Edits rules.json in place and restores it at the end.
set -uo pipefail
cd "$(dirname "$0")"
unset DOCKER_HOST
N=localhost:18081
H=(-s --max-time 90 -H Host:www.cru.org -H X-Forwarded-Proto:https -H X-Forwarded-For:203.0.113.9)
cp rules.json rules.json.bak
trap 'cat rules.json.bak > rules.json; rm -f rules.json.bak' EXIT

# Rewrite rules.json in place (same inode) so the bind mount sees the change.
edit() {
  python3 - "$@" <<'PY'
import json, sys
d = json.load(open("rules.json"))
op = sys.argv[1]
if op == "vanity": d["vanities"][sys.argv[2]] = sys.argv[3]
if op == "forward": d["forward_query"] = sys.argv[2:]
if op == "empty": d["vanities"] = {}
with open("rules.json", "r+") as f:
    f.truncate(0)
    json.dump(d, f)
PY
}

echo "1. vip hung:"; docker compose pause vip >/dev/null 2>&1
curl "${H[@]}" -o /tmp/h1 -w '   %{http_code} t=%{time_total}s ' $N/wp-admin/new1; head -c 60 /tmp/h1; echo
docker compose unpause vip >/dev/null 2>&1

echo "2. purge picks up a new vanity:"; edit vanity /harness-test /harness-ok; sleep 2.5
curl "${H[@]}" -o /dev/null -w '   purge: %{http_code} %header{location}\n' "$N/harness-test?purge_vanity=1"
curl "${H[@]}" "$N/communities/x?purge_target=1" | python3 -c 'import json,sys;print("   purge_target proxied path:",json.load(sys.stdin)["path"])'

echo "3. forward query opt-in:"; sleep 3; edit forward /10steps /optionstogetherchi
curl "${H[@]}" -o /dev/null -w '   /10steps: %header{location}\n' "$N/10steps?utm_source=a&purge_vanity=1"
curl "${H[@]}" -o /dev/null -w '   /optionstogetherchi: %header{location}\n' "$N/optionstogetherchi?e=9&utm_source=a"
curl "${H[@]}" -o /dev/null -w '   /7steps (not opted in): %header{location}\n' "$N/7steps?utm_source=a"

echo "4. emptied document:"; sleep 3; edit empty
curl "${H[@]}" -o /dev/null -w '   after purge: %{http_code} %header{location}\n' "$N/10steps?purge_vanity=1"
curl "${H[@]}" -o /dev/null -w '   health: %{http_code}\n' $N/monitor.html

echo "5. start with an unreadable rules file:"
cat rules.json.bak > rules.json
docker compose exec -T new /cruproxy healthcheck; echo "   healthcheck subcommand exit: $?"
docker compose stop new >/dev/null 2>&1; : > rules.json; docker compose start new >/dev/null 2>&1; sleep 2
curl "${H[@]}" -o /dev/null -w '   health while unreadable: %{http_code}\n' $N/monitor.html
cat rules.json.bak > rules.json; sleep 4
curl "${H[@]}" -o /dev/null -w '   health after fix: %{http_code}\n' $N/monitor.html
