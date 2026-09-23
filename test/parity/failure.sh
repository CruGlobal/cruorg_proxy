#!/usr/bin/env bash
# Failure-mode and feature checks against one proxy. Usage: ./failure.sh [port]
set -uo pipefail
cd "$(dirname "$0")"
unset DOCKER_HOST
N=localhost:${1:-18081}
H=(-s --max-time 90 -H Host:www.cru.org -H X-Forwarded-Proto:https -H X-Forwarded-For:203.0.113.9)
R() { docker compose exec -T redis redis-cli -n 3 "$@" >/dev/null; }
echo "1. vip hung:"; docker compose pause vip >/dev/null 2>&1
curl "${H[@]}" -o /tmp/h1 -w '   %{http_code} t=%{time_total}s ' $N/wp-admin/new1; head -c 70 /tmp/h1; echo
docker compose unpause vip >/dev/null 2>&1
echo "2. redis hung:"; docker compose pause redis >/dev/null 2>&1
curl "${H[@]}" -o /dev/null -w '   uncached vanity /7steps: %{http_code} %header{location} t=%{time_total}s\n' $N/7steps
curl "${H[@]}" -w '   uncached wp path /mycru/x t=%{time_total}s ' $N/mycru/x | head -c 25; echo
curl "${H[@]}" -o /dev/null -w '   purge while down: %{http_code} t=%{time_total}s\n' "$N/10steps?purge_vanity=1"
curl "${H[@]}" -o /dev/null -w '   health: %{http_code}\n' $N/monitor.html
docker compose unpause redis >/dev/null 2>&1; sleep 11
echo "3. purge picks up a new vanity:"; R hset cruorg:vanities /harness-test /harness-ok
curl "${H[@]}" -o /dev/null -w '   no purge: %{http_code}\n' $N/harness-test
curl "${H[@]}" -o /dev/null -w '   purge: %{http_code} %header{location}\n' "$N/harness-test?purge_vanity=1"
curl "${H[@]}" "$N/communities/x?purge_target=1" | python3 -c 'import json,sys;print("   purge_target proxied path:",json.load(sys.stdin)["path"])'
echo "4. forward query opt-in:"; R hset cruorg:forward_query /10steps 1 /optionstogetherchi 1; sleep 11
curl "${H[@]}" -o /dev/null -w '   /10steps: %header{location}\n' "$N/10steps?utm_source=a&purge_vanity=1"
curl "${H[@]}" -o /dev/null -w '   /optionstogetherchi: %header{location}\n' "$N/optionstogetherchi?e=9&utm_source=a"
curl "${H[@]}" -o /dev/null -w '   /7steps (not opted in): %header{location}\n' "$N/7steps?utm_source=a"
echo "5. vanity hash deleted:"; R rename cruorg:vanities cruorg:vanities-bak; sleep 11
curl "${H[@]}" -o /dev/null -w '   after purge: %{http_code} %header{location}\n' "$N/10steps?purge_vanity=1"
R rename cruorg:vanities-bak cruorg:vanities; R hdel cruorg:vanities /harness-test; R del cruorg:forward_query
echo "6. start while redis down:"; docker compose pause redis >/dev/null 2>&1; docker compose restart new >/dev/null 2>&1; sleep 3
curl "${H[@]}" -o /dev/null -w '   health while down: %{http_code}\n' $N/monitor.html
docker compose unpause redis >/dev/null 2>&1; sleep 5
curl "${H[@]}" -o /dev/null -w '   health after redis back: %{http_code}\n' $N/monitor.html
