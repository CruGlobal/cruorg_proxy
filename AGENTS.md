# Working in this repo (for coding agents)

This repository is **`cruorg_proxy`**, the proxy behind **www.cru.org** and
**stage.cru.org**. It is an OpenResty (nginx + Lua) image that redirects vanity
and legacy paths and sends everything else to **AEM** (the default) or
**WordPress VIP** (matched paths).

Traffic path: CloudFront (WAF, geo function) → ALB (WAF, origin-verify header)
→ this container on port 80 → AEM or WordPress VIP. TLS ends at the ALB.
Health check: `GET /monitor.html`.

The redirect and upstream rules live in **Redis**, read at request time by
`usr/local/openresty/nginx/conf/redirect.lua` and `target.lua`:

- `VANITY_KEY` hash: exact path (lowercased) → redirect target (301)
- `REWRITES_KEY` hash: regex → replacement, longest pattern first (301)
- `UPSTREAMS_KEY` hash: regex → upstream env var name (default `DEFAULT_PROXY_TARGET`)

Results are cached per path for an hour. `?purge_vanity` / `?purge_target`
skip the cache for that request, which is handy when testing a new rule.

## The loop

- Build the image the way CI does: `./build.sh` (`docker buildx build`).
- There is no test suite. `Validate PR Title` is the only required check.
- `openresty-opm` in the Dockerfile is pinned to the base image's exact
  openresty version. Bump both together or the build fails.

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
  **Redis hashes themselves** (`stage/redirects.tf`, `prod/redirects.tf`).
  Adding a redirect or upstream rule is a cru-terraform PR, never a hand edit in
  Redis.
- Runtime settings (`DEFAULT_PROXY_TARGET`, `VIP_ADDR`, the hash keys) are ECS
  parameters there; `AEM_EDGE_KEY` is a secret in SSM under
  `/ecs/cruorg_proxy/<env>/`. Never commit secrets.
- A Terraform change to parameters or the task definition lands on the **next**
  deploy. If nothing else changed, use cru-deploy's **Deploy Candidate (v2)**
  with `force: true`.

## Leftovers you can ignore

- `.github/workflows/build-deploy-ecs.yml` is the **parked v1 workflow**,
  dispatch-only on purpose. Its build fails with `AccessDenied` now that v1's
  build identity is retired; keep it as the record of how the app used to ship.
- That workflow pins `CruGlobal/.github` at `@v1`. **Don't bump it to `@v2`**:
  v2 of those workflows is a different pipeline. Dependabot is set to ignore
  that major version; close any PR that proposes it.
- There is no `staging` branch or `On Staging` label any more. Anything that
  mentions them, or a merge-bot, describes the v1 flow.
- A rebuild of this proxy in Go, with the rules moving out of Redis, is in
  progress separately. Until it merges, this file describes the OpenResty app.

## If you're not sure what to do

- **This is www.cru.org.** Keep changes small, on a branch, behind a PR.
- **Don't invent infrastructure.** New parameters, DNS, redirects or upstreams
  are a cru-terraform change.
- **Confirm before anything outward-facing or hard to undo**: pushing, opening
  PRs, dispatching a promote or rollback, deleting things.
