FROM golang:1.25.6-alpine AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY cmd/ cmd/
COPY internal/ internal/
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/cruproxy ./cmd/cruproxy

FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=builder /out/cruproxy /cruproxy

USER nonroot:nonroot

LABEL com.datadoghq.ad.check_names='["openmetrics"]'
LABEL com.datadoghq.ad.init_configs='[{}]'
LABEL com.datadoghq.ad.instances='[{"openmetrics_endpoint": "http://%%host%%:6000/metrics","namespace":"cruproxy","metrics":["cruproxy_.*","go_goroutines","go_memstats_heap_inuse_bytes","process_cpu_seconds_total"]}]'
LABEL com.datadoghq.ad.logs='[{"source": "go"}]'

HEALTHCHECK --interval=10s --timeout=5s CMD ["/cruproxy", "healthcheck"]

EXPOSE 80 6000

ENTRYPOINT ["/cruproxy"]

ARG VERSION="dev"
ENV DD_VERSION=${VERSION}
