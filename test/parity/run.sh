#!/usr/bin/env bash
# Side-by-side parity run: the last OpenResty image (reading Redis) vs this
# repo's Go build (reading rules.json), both built from the same redirects.tf.
# Usage: test/parity/run.sh <cru-terraform>/applications/cruorg_proxy/prod/redirects.tf
set -euo pipefail
cd "$(dirname "$0")"
TF=${1:?path to redirects.tf}
mkdir -p certs
[ -f certs/cert.pem ] || openssl req -x509 -newkey rsa:2048 -nodes -days 30 -subj "/CN=upstream" \
  -addext "subjectAltName=DNS:publish-p56256-e778627.adobeaemcloud.com,DNS:wordpress.cru.org" \
  -keyout certs/key.pem -out certs/cert.pem 2>/dev/null
python3 load.py "$TF" > resp.txt
docker image inspect cruorg-proxy-old:local >/dev/null 2>&1 || docker build -t cruorg-proxy-old:local "https://github.com/CruGlobal/cruorg_proxy.git#78e732d"
COMPOSE_BAKE=false docker compose build new
docker compose up -d
docker compose exec -T redis redis-cli -n 3 --pipe < resp.txt
sleep 3
python3 probe.py 18080 run-old.jsonl
python3 probe.py 18081 run-new.jsonl
python3 compare.py run-old.jsonl run-new.jsonl
