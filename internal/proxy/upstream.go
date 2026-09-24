package proxy

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"
)

// Timeouts for upstream requests. The response timeout matches nginx's
// proxy_read_timeout default; a slower upstream gets a 504.
const (
	dialTimeout           = 5 * time.Second
	tlsTimeout            = 5 * time.Second
	responseHeaderTimeout = 60 * time.Second
	keepAlive             = 30 * time.Second
	idleConnTimeout       = 90 * time.Second
	maxIdlePerHost        = 64
)

// UpstreamConfig describes one place the proxy sends requests.
type UpstreamConfig struct {
	Name string
	URL  string
	// OriginHost sends the upstream's own hostname as Host. AEM's CDN requires
	// it; WordPress VIP serves by the visitor's host.
	OriginHost bool
	// Headers are set on every request to this upstream only.
	Headers map[string]string
	// RootCAs overrides the system roots; tests use it for a local TLS server.
	RootCAs *x509.CertPool
}

// NewUpstream builds a reverse proxy for one upstream. TLS is verified and the
// server name is the upstream's hostname, whatever Host header is sent.
func NewUpstream(cfg UpstreamConfig, trusted TrustedProxies, logger *slog.Logger) (*httputil.ReverseProxy, error) {
	target, err := url.Parse(cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("upstream %s: %w", cfg.Name, err)
	}
	if target.Scheme == "" || target.Host == "" {
		return nil, fmt.Errorf("upstream %s: %q is not an absolute URL", cfg.Name, cfg.URL)
	}
	transport := &http.Transport{
		DialContext:           (&net.Dialer{Timeout: dialTimeout, KeepAlive: keepAlive}).DialContext,
		TLSHandshakeTimeout:   tlsTimeout,
		ResponseHeaderTimeout: responseHeaderTimeout,
		MaxIdleConnsPerHost:   maxIdlePerHost,
		IdleConnTimeout:       idleConnTimeout,
		TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: cfg.RootCAs},
	}
	return &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(target)
			if !cfg.OriginHost {
				pr.Out.Host = pr.In.Host
			}
			xff := strings.Join(pr.In.Header.Values("X-Forwarded-For"), ", ")
			if xff != "" {
				xff += ", "
			}
			pr.Out.Header.Set("X-Forwarded-For", xff+PeerIP(pr.In))
			pr.Out.Header.Set("X-Forwarded-Host", pr.In.Host)
			proto := pr.In.Header.Get("X-Forwarded-Proto")
			if proto != "http" && proto != "https" {
				proto = "http"
			}
			pr.Out.Header.Set("X-Forwarded-Proto", proto)
			pr.Out.Header.Set("X-Real-Ip", trusted.ClientIP(pr.In))
			for k, v := range cfg.Headers {
				pr.Out.Header.Set(k, v)
			}
		},
		Transport: transport,
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, err error) {
			status := http.StatusBadGateway
			var ne net.Error
			if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &ne) && ne.Timeout()) {
				status = http.StatusGatewayTimeout
			}
			logger.Error(
				"upstream error",
				slog.String("upstream", cfg.Name),
				slog.Int("status", status),
				slog.Any("error", err),
			)
			ErrorPage(w, status)
		},
	}, nil
}

// ErrorPage writes the proxy's own error page.
func ErrorPage(w http.ResponseWriter, status int) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(
		[]byte("<!doctype html><title>Cru</title><h1>Something went wrong</h1><p>Please try again in a moment.</p>\n"),
	)
}
