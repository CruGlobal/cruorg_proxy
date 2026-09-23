# Working in this repo (for coding agents)

This repository is **`cruorg_proxy`**, the proxy behind **www.cru.org** and
**stage.cru.org**. It is a **custom [Caddy](https://caddyserver.com) build**
written in Go: the `cruproxy` binary is Caddy plus one handler module that
redirects vanity and legacy paths and sends everything else to **AEM** (the
default) or **WordPress VIP** (matched paths).

Traffic path: CloudFront (WAF, geo function) → ALB (WAF, origin-verify header)
→ this container on port 80 → AEM or WordPress VIP. TLS ends at the ALB; the
container speaks plain HTTP. Health check: `GET /monitor.html`.

## How requests are handled

1. `/monitor.html` returns 200 once rules are loaded, 503 before.
2. `/cru-nav.js` is rewritten to `/cru-nav.json`.
3. The `cruproxy` handler looks up the normalized path (slashes merged, dot
   segments resolved):
   - exact match, lowercased, in the **vanities** hash → 301
   - else the first match in the **rewrites** hash (regex, case-insensitive,
     longest pattern first) → 301
   - else it sets `{http.cruproxy.upstream}` from the **upstreams** hash
     (default `DEFAULT_PROXY_TARGET`) and the Caddyfile routes on it.
4. AEM gets `Host` = its origin host plus `X-AEM-Edge-Key`. WordPress VIP gets
   the visitor's `Host` and never the AEM key.

Rules live in **Redis** (db index from `STORAGE_REDIS_DB_INDEX`), load into
memory at startup and refresh every 60s. A failed or empty load keeps the last
good copy. `?purge_vanity` or `?purge_target` forces a reload, at most once per
10s per task; a purge only reaches the task that served the request.

Redirects drop the query string unless their key (vanity path or regex
pattern) is in the optional **forward_query** hash; then the incoming query is
merged into the target's, target params winning.

## Layout

```
.
├── Caddyfile            # the shipped server config
├── Dockerfile           # caddy:<ver>-builder-alpine -> caddy:<ver>-alpine
├── build.sh             # what CI runs to build the image
├── Taskfile.yml         # build / fmt / lint / test / caddy:validate / parity:run
├── .tool-versions       # asdf: caddy, golang, golangci-lint, pre-commit
├── cmd/cruproxy/        # the binary (Caddy's cmd wiring)
├── internal/cruproxy/   # the HTTP handler, path normalization, query merge
├── internal/store/      # Redis loader and rule matching
└── test/parity/         # side-by-side harness against the old OpenResty image
```

**The Caddy version lives in `.tool-versions`.** `build.sh` passes it as
`--build-arg CADDY_VERSION`. Bump the Caddy Go module in `go.mod` with it.

## The loop

| Command | What it does |
| --- | --- |
| `task build` | build `./cruproxy` |
| `task fmt` / `task lint` | `golangci-lint fmt` / `run --fix` (CI fails on any diff) |
| `task test` | unit tests |
| `task caddy:validate` | validate the shipped `Caddyfile` |
| `task parity:run -- <redirects.tf>` | old vs new side by side, see `test/parity/README.md` |

`CGO_ENABLED=0` is set in the Taskfile; nothing here needs cgo.

Required status checks (the `pipeline-v2-default-branch` ruleset, configured in
cru-terraform): **`Build / Format / Lint`**, **`Test`**, **`Validate PR Title`**.
Renaming a job silently drops its gate, so change cru-terraform with it.

## How this app ships

Pipeline v2: build once from the default branch, deploy to
**release-candidate** (stage.cru.org), then **promote** the same image by digest
to production. Reference:
[`docs/pipeline-v2.md`](https://github.com/CruGlobal/cru-deploy/blob/main/docs/pipeline-v2.md).

1. Branch off the default branch, open a PR. The repo is squash-only with
   auto-merge; the PR title must be a Conventional Commit.
2. Builds do not run on push. `pipeline-v2.yml` runs nightly at 05:00 UTC (or
   on dispatch) and deploys the candidate to release-candidate.
3. Production is a manual **Promote (v2)** dispatch in cru-deploy.

There is no `staging` branch, `On Staging` label, or merge-bot any more.
`build-deploy-ecs.yml` is the parked v1 workflow and stays dispatch-only.

## Infrastructure

Everything else is Terraform in
[`cru-terraform`](https://github.com/CruGlobal/cru-terraform) under
`applications/cruorg_proxy/`: the ECS service, ALB, CloudFront, WAF, and the
**Redis hashes themselves** (`stage/redirects.tf`, `prod/redirects.tf`). Adding a
redirect or upstream rule is a cru-terraform PR, never a hand edit in Redis.
Runtime settings (`DEFAULT_PROXY_TARGET`, `VIP_ADDR`, the hash keys) are ECS
parameters there; `AEM_EDGE_KEY` is an SSM secret.

## Be careful with

- **This is www.cru.org.** Every change affects the main site. Run the parity
  harness before changing matching, headers, or the Caddyfile routes.
- **Vanity keys must be lowercase.** The lookup lowercases the path, so a
  mixed-case key never matches.
- **Caddy verifies upstream TLS.** Don't turn that off to make a test pass.
- **Failure tests use `docker compose pause`, not `stop`.** A stopped fake
  upstream loses its DNS alias and the request goes to the real site.
- Confirm before anything outward-facing: pushing, opening PRs, dispatching a
  promote or rollback.
