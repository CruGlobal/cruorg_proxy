# Working in this repo (for coding agents)

This repository is **`cruorg_proxy`**, the proxy behind **www.cru.org** and
**stage.cru.org**. It is a small Go service (standard library `net/http` and
`httputil.ReverseProxy`) that redirects vanity and legacy paths and sends
everything else to **AEM** (the default) or **WordPress VIP** (matched paths).

Traffic path: CloudFront (WAF, geo function) → ALB (WAF, origin-verify header)
→ this container on port 80 → AEM or WordPress VIP. TLS ends at the ALB.
Health check: `GET /monitor.html` (200 once rules are loaded, 503 before).

## How requests are handled

1. `/cru-nav.js` is served as `/cru-nav.json`.
2. The path is normalized the way nginx did (slashes merged, dot segments
   resolved), then:
   - exact match, lowercased, in **vanities** → 301
   - else the first match in **rewrites** (regex, case-insensitive, longest
     pattern first) → 301
   - else **upstreams** (regex on the lowercased path) picks the upstream by
     env var name; the default is `DEFAULT_PROXY_TARGET`.
3. AEM gets `Host` = its origin host plus `X-AEM-Edge-Key`. WordPress VIP gets
   the visitor's `Host` and never the AEM key. Upstream TLS is verified.
4. The raw path and query go upstream unchanged.

Redirects drop the query string unless their key (vanity path or regex pattern)
is listed in `forward_query`; then the incoming query is merged into the
target's, target params winning.

## Where the rules come from

One JSON document, `rules.json`, written by cru-terraform to S3
(`RULES_BUCKET` / `RULES_KEY`). The proxy loads it at startup and checks it
every 60s with a conditional GET, so an unchanged object costs no download. A
failed, unparsable, or emptied document keeps the last good copy.
`?purge_vanity` or `?purge_target` forces a check now, at most once per 10s per
task; a purge only reaches the task that served the request. For local runs,
`RULES_FILE` points at a file instead.

```json
{"vanities": {"/10steps": "/train-and-grow/10-basic-steps.html"},
 "rewrites": {"^/campus/(.*)": "/communities/campus/$1"},
 "upstreams": {"^/wp-": "VIP_ADDR"},
 "forward_query": []}
```

## Layout

```
.
├── cmd/cruproxy/      # main: config from env, servers, the healthcheck subcommand
├── internal/proxy/    # handler, upstreams, client IP, path + query, logging, metrics
├── internal/store/    # rules document, matching, S3 and file sources
├── test/parity/       # side-by-side harness against the last OpenResty image
├── Dockerfile         # golang builder -> distroless static, non-root
└── Taskfile.yml       # build / fmt / lint / test / run / parity:run
```

Logs are JSON on stdout with Datadog's standard attributes (`http.status_code`,
`network.client.ip`, `duration`). Metrics are Prometheus text on `:6000/metrics`
(`cruproxy_*`), scraped by Datadog's openmetrics check.

## The loop

| Command | What it does |
| --- | --- |
| `task build` | build `./cruproxy` |
| `task fmt` / `task lint` | `golangci-lint fmt` / `run --fix` (CI fails on any diff) |
| `task test` | unit tests |
| `task run -- rules.json` | run locally on :8080 against a rules file |
| `task parity:run -- <redirects.tf>` | old vs new side by side; see `test/parity/README.md` |

Required status checks (the `pipeline-v2-default-branch` ruleset, configured in
cru-terraform): **`Build / Format / Lint`**, **`Test`**, **`Validate PR Title`**.
Renaming a job silently drops its gate, so change cru-terraform with it.

## How this app ships

This repo is on **pipeline v2: build once, then promote the artifact**. The
reference is [`docs/pipeline-v2.md`](https://github.com/CruGlobal/cru-deploy/blob/main/docs/pipeline-v2.md)
in `CruGlobal/cru-deploy`.

1. Branch off `main` and open a PR back to `main`. The repo is squash-only with
   auto-merge; one approving review is required because this proxy fronts all of
   www.cru.org.
2. Write the PR title as a Conventional Commit (`feat: …`, `fix(redirect): …`).
   It becomes the squash commit subject and feeds the deploy changelog.
3. **Builds do not run on push.** `.github/workflows/pipeline-v2.yml` runs
   nightly at 05:00 UTC (or on dispatch), builds one environment-agnostic
   `candidate-*` image, and deploys it to **release-candidate** (stage.cru.org).
4. **Production is a manual promote**: dispatch **Promote (v2)** in
   `CruGlobal/cru-deploy` (it checks you have push access here). Rollback is
   `rollback.yml` there. Promoted images get a permanent `release-*` tag.

Deploy, promote, rollback and failure notices go to **#devops-notifications**.
The image is identical in every environment; the only baked-in value is
`DD_VERSION` (the build number). Everything else arrives at runtime.

There are **no database migrations**; Terraform declares
`database_migrations = { enabled = false }`, so promotes report
`safe — no database migrations`.

## Infrastructure & secrets

- Everything is Terraform in
  [`cru-terraform`](https://github.com/CruGlobal/cru-terraform) under
  `applications/cruorg_proxy/`: the ECS service, ALB, CloudFront, WAF, and the
  **rules themselves** (the maps in `stage/redirects.tf` and
  `prod/redirects.tf`, written to `rules.json`). Adding a redirect or upstream
  rule is a cru-terraform PR; the plan shows the per-key diff.
- Runtime settings (`DEFAULT_PROXY_TARGET`, `VIP_ADDR`, `RULES_BUCKET`,
  `RULES_KEY`) are ECS parameters there; `AEM_EDGE_KEY` is a secret in SSM
  under `/ecs/cruorg_proxy/<env>/`. Never commit secrets.
- A Terraform change to parameters or the task definition lands on the **next**
  deploy. If nothing else changed, use cru-deploy's **Deploy Candidate (v2)**
  with `force: true`. A change to the rules themselves needs no deploy.

| Variable | Default | Purpose |
| --- | --- | --- |
| `RULES_BUCKET`, `RULES_KEY` | `rules.json` | S3 location of the rules |
| `RULES_FILE` | | local file instead of S3 |
| `RULES_REFRESH`, `RULES_MIN_RELOAD` | `60s`, `10s` | check interval, purge rate limit |
| `DEFAULT_PROXY_TARGET`, `VIP_ADDR` | | AEM and WordPress VIP origins |
| `AEM_EDGE_KEY` | | sent to AEM only |
| `TRUSTED_PROXIES` | `10.16.0.0/16` | CIDRs allowed to set X-Forwarded-For |
| `LISTEN_ADDR`, `METRICS_ADDR` | `:80`, `:6000` | |
| `LOG_LEVEL` | `INFO` | |

## Leftovers you can ignore

- `.github/workflows/build-deploy-ecs.yml` is the **parked v1 workflow**,
  dispatch-only on purpose. Its build fails with `AccessDenied` now that v1's
  build identity is retired; keep it as the record of how the app used to ship.
- That workflow pins `CruGlobal/.github` at `@v1`. **Don't bump it to `@v2`**:
  v2 of those workflows is a different pipeline. Dependabot ignores that major
  version; close any PR that proposes it.
- There is no `staging` branch or `On Staging` label any more. Anything that
  mentions them, or a merge-bot, describes the v1 flow.
- Until cleanup finishes, cru-terraform still writes the old Redis hashes too,
  and the ECS parameters still carry `STORAGE_REDIS_*` and the `*_KEY` hash
  names. This proxy ignores them.

## If you're not sure what to do

- **This is www.cru.org.** Keep changes small, on a branch, behind a PR, and run
  the parity harness before changing matching, headers, or upstream handling.
- **Vanity keys must be lowercase.** The lookup lowercases the path, so a
  mixed-case key never matches.
- **Failure tests use `docker compose pause`, not `stop`.** A stopped fake
  upstream loses its DNS alias and the request goes to the real site.
- **Don't invent infrastructure.** New parameters, DNS, redirects or upstreams
  are a cru-terraform change.
- **Confirm before anything outward-facing or hard to undo**: pushing, opening
  PRs, dispatching a promote or rollback, deleting things.
