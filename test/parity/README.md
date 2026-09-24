# Parity harness

Runs the last OpenResty image (`old`, port 18080, built from commit 78e732d and
reading Redis) and this repo's Go build (`new`, port 18081, reading
`rules.json`) side by side. Both get their rules from the same cru-terraform
`redirects.tf`, via `load.py`. Fake TLS upstreams answer as the AEM and
WordPress hostnames and echo back the Host, SNI, headers and path they got.

    test/parity/run.sh ../cru-terraform/applications/cruorg_proxy/prod/redirects.tf
    test/parity/failure.sh

`run.sh` probes about 3,200 URLs against both proxies and prints every field
that differs, including X-Forwarded-For and X-Real-IP. Expected differences:
`X-AEM-Edge-Key` is not sent to WordPress; trailing whitespace in a stored
target is trimmed; and `/CAMPUS/SEATTLE/page` keeps the visitor's case where
nginx served a cached lowercase answer. `baseline-old.jsonl` is the old proxy's
result on prod data from cru-terraform 713d9b521.

`failure.sh` covers a hung upstream, purge, query forwarding, an emptied rules
document, and starting with an unreadable rules file. Metrics are on
`localhost:16000/metrics`.

Use `docker compose pause`, not `stop`, for failure tests. Stopping an upstream
removes its network alias, and Docker DNS then resolves the real public
hostname, so requests leak to the real site.
