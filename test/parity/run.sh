#!/usr/bin/env bash
# Side-by-side parity run: old OpenResty proxy vs the Caddy build.
# Usage: test/parity/run.sh <path to cru-terraform>/applications/cruorg_proxy/prod/redirects.tf
set -euo pipefail
cd "$(dirname "$0")"
TF=${1:?path to redirects.tf}
mkdir -p certs
[ -f certs/cert.pem ] || openssl req -x509 -newkey rsa:2048 -nodes -days 30 -subj "/CN=upstream" \
  -addext "subjectAltName=DNS:publish-p56256-e778627.adobeaemcloud.com,DNS:wordpress.cru.org" \
  -keyout certs/key.pem -out certs/cert.pem 2>/dev/null
docker compose up -d --build
python3 load.py "$TF" > resp.txt
docker compose exec -T redis redis-cli -n 3 --pipe < resp.txt
docker compose restart new >/dev/null
sleep 3
python3 probe.py 18080 run-old.jsonl
python3 probe.py 18081 run-new.jsonl
python3 compare.py run-old.jsonl run-new.jsonl
