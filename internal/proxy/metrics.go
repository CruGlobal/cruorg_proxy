package proxy

import (
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/CruGlobal/cruorg_proxy/internal/store"
)

// Metrics are served on the metrics port for Datadog's openmetrics check.
type Metrics struct {
	registry    *prometheus.Registry
	requests    *prometheus.CounterVec
	duration    *prometheus.HistogramVec
	loads       *prometheus.CounterVec
	ruleCount   *prometheus.GaugeVec
	rulesLoaded prometheus.Gauge
}

// NewMetrics registers the proxy's metrics plus Go runtime and process ones.
func NewMetrics() *Metrics {
	m := &Metrics{
		registry: prometheus.NewRegistry(),
		requests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "cruproxy_requests_total",
			Help: "Requests by kind (proxy, redirect, health), upstream and status code.",
		}, []string{"kind", "upstream", "code"}),
		duration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "cruproxy_request_duration_seconds",
			Help:    "Request duration by kind and upstream.",
			Buckets: prometheus.DefBuckets,
		}, []string{"kind", "upstream"}),
		loads: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "cruproxy_rules_loads_total",
			Help: "Rules loads by result (changed, unchanged, error).",
		}, []string{"result"}),
		ruleCount: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "cruproxy_rules",
			Help: "Rules in the current snapshot, by type.",
		}, []string{"type"}),
		rulesLoaded: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "cruproxy_rules_loaded_timestamp_seconds",
			Help: "Unix time the current rules snapshot was loaded.",
		}),
	}
	m.registry.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		m.requests, m.duration, m.loads, m.ruleCount, m.rulesLoaded,
	)
	return m
}

func (m *Metrics) observe(kind, upstream string, status int, d time.Duration) {
	m.requests.WithLabelValues(kind, upstream, strconv.Itoa(status)).Inc()
	m.duration.WithLabelValues(kind, upstream).Observe(d.Seconds())
}

// LoadResult records the outcome of one rules load.
func (m *Metrics) LoadResult(changed bool, err error, snap *store.Snapshot) {
	switch {
	case err != nil:
		m.loads.WithLabelValues("error").Inc()
		return
	case !changed:
		m.loads.WithLabelValues("unchanged").Inc()
		return
	}
	m.loads.WithLabelValues("changed").Inc()
	if snap == nil {
		return
	}
	m.ruleCount.WithLabelValues("vanity").Set(float64(len(snap.Vanities)))
	m.ruleCount.WithLabelValues("rewrite").Set(float64(len(snap.Rewrites)))
	m.ruleCount.WithLabelValues("upstream").Set(float64(len(snap.Upstreams)))
	m.ruleCount.WithLabelValues("forward_query").Set(float64(len(snap.ForwardQuery)))
	m.rulesLoaded.Set(float64(snap.LoadedAt.Unix()))
}

// Handler serves the metrics in Prometheus text format.
func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{})
}
