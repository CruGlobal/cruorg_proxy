# Parity harness

Runs the last OpenResty image (`old`, port 18080, built from commit 78e732d)
and this repo's Caddy build (`new`, port 18081) against one Redis loaded from
cru-terraform's `redirects.tf`. Fake TLS upstreams answer as the AEM and
WordPress hostnames and echo back the Host, SNI, headers and path they got.

    test/parity/run.sh ../cru-terraform/applications/cruorg_proxy/prod/redirects.tf
    test/parity/failure.sh 18081

`run.sh` probes about 3,200 URLs against both proxies and prints every field
that differs. Expected differences: `X-AEM-Edge-Key` is not sent to WordPress,
and trailing whitespace in a stored target is trimmed. `baseline-old.jsonl` is
the old proxy's result on prod data from cru-terraform 713d9b521.

`failure.sh` covers a hung upstream, hung Redis, purge params, query
forwarding, an emptied hash, and starting while Redis is down.

Use `docker compose pause`, not `stop`, for failure tests. Stopping an upstream
removes its network alias, and Docker DNS then resolves the real public
hostname, so requests leak to the real site.
