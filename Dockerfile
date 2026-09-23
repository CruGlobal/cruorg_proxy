# CADDY_VERSION set by build.sh from .tool-versions
ARG CADDY_VERSION=0
FROM public.ecr.aws/docker/library/caddy:${CADDY_VERSION}-builder-alpine AS builder

WORKDIR /usr/src/cruproxy

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -v -o /usr/bin/cruproxy ./cmd/cruproxy

ARG CADDY_VERSION=0
FROM public.ecr.aws/docker/library/caddy:${CADDY_VERSION}-alpine

COPY --from=builder /usr/bin/cruproxy /usr/bin/cruproxy

LABEL com.datadoghq.ad.check_names='["openmetrics"]'
LABEL com.datadoghq.ad.init_configs='[{}]'
LABEL com.datadoghq.ad.instances='[{"openmetrics_endpoint": "http://%%host%%:6000/metrics","namespace":"caddy","metrics":["caddy_http_.*"]}]'
LABEL com.datadoghq.ad.logs='[{"source": "caddy"}]'

HEALTHCHECK --interval=10s --timeout=5s \
  CMD wget -q --tries=1 --spider http://127.0.0.1/monitor.html || exit 1

RUN apk upgrade --no-cache

COPY Caddyfile /etc/caddy/Caddyfile

EXPOSE 80

CMD ["cruproxy", "run", "--config", "/etc/caddy/Caddyfile", "--adapter", "caddyfile"]

ARG VERSION="dev"
ENV DD_VERSION=${VERSION}
