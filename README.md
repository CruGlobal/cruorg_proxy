# cruorg_proxy

The proxy behind www.cru.org: a custom Caddy build that redirects vanity and
legacy paths and sends everything else to AEM or WordPress VIP, based on rules
kept in Redis and managed in cru-terraform.

See [AGENTS.md](./AGENTS.md) for how it works, how to build and test it, and
how it ships.
