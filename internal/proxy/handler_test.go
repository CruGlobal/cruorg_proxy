package proxy_test

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/CruGlobal/cruorg_proxy/internal/proxy"
	"github.com/CruGlobal/cruorg_proxy/internal/store"
)

type seen struct {
	Name    string `json:"name"`
	Host    string `json:"host"`
	SNI     string `json:"sni"`
	Path    string `json:"path"`
	XFF     string `json:"xff"`
	XFP     string `json:"xfp"`
	XFH     string `json:"xfh"`
	XRI     string `json:"xri"`
	EdgeKey string `json:"edge_key"`
}

// echo is a TLS upstream that reports what it received.
func echo(t *testing.T, name string) *httptest.Server {
	t.Helper()
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(seen{
			Name: name, Host: r.Host, SNI: r.TLS.ServerName, Path: r.URL.RequestURI(),
			XFF: r.Header.Get("X-Forwarded-For"), XFP: r.Header.Get("X-Forwarded-Proto"),
			XFH: r.Header.Get("X-Forwarded-Host"), XRI: r.Header.Get("X-Real-Ip"),
			EdgeKey: r.Header.Get("X-Aem-Edge-Key"),
		})
	}))
	srv.TLS = &tls.Config{MinVersion: tls.VersionTLS12}
	srv.StartTLS()
	t.Cleanup(srv.Close)
	return srv
}

type memSource struct{ body []byte }

func (m memSource) Fetch(_ context.Context, etag string) ([]byte, string, error) {
	if etag == "1" {
		return nil, etag, store.ErrNotModified
	}
	return m.body, "1", nil
}

func newProxy(t *testing.T, loaded bool) (http.Handler, *httptest.Server) {
	t.Helper()
	aem, vip := echo(t, "aem"), echo(t, "vip")
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	trusted, _ := proxy.ParseTrusted("10.16.0.0/16")

	ups := map[string]http.Handler{}
	for name, cfg := range map[string]proxy.UpstreamConfig{
		store.DefaultUpstream: {Name: "aem", URL: aem.URL, OriginHost: true, Headers: map[string]string{"X-AEM-Edge-Key": "k"}},
		"VIP_ADDR":            {Name: "vip", URL: vip.URL},
	} {
		srv := aem
		if name == "VIP_ADDR" {
			srv = vip
		}
		cfg.RootCAs = srv.Client().Transport.(*http.Transport).TLSClientConfig.RootCAs
		rp, err := proxy.NewUpstream(cfg, trusted, logger)
		if err != nil {
			t.Fatal(err)
		}
		ups[name] = rp
	}

	body, _ := json.Marshal(store.Document{
		Vanities: map[string]string{
			"/10steps": "/train-and-grow/10-basic-steps.html",
			"/fund":    "https://x.example/e/?e=31709",
		},
		Rewrites:     map[string]string{"^/campus/(.*)": "/communities/campus/$1"},
		Upstreams:    map[string]string{"^/wp-": "VIP_ADDR"},
		ForwardQuery: []string{"/fund"},
	})
	st := store.New(memSource{body: body}, 0, nil)
	if loaded {
		if _, err := st.Load(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	h := &proxy.Handler{Store: st, Upstreams: ups, HealthPath: "/monitor.html", Logger: logger}
	return proxy.Middleware(h, logger, proxy.NewMetrics(), trusted, "/monitor.html"), aem
}

func do(h http.Handler, path string, hdr map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Host = "www.cru.org"
	req.RemoteAddr = "192.168.48.1:5555"
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func decode(t *testing.T, rec *httptest.ResponseRecorder) seen {
	t.Helper()
	var s seen
	if err := json.Unmarshal(rec.Body.Bytes(), &s); err != nil {
		t.Fatalf("status %d body %q: %v", rec.Code, rec.Body.String(), err)
	}
	return s
}

func TestHealth(t *testing.T) {
	h, _ := newProxy(t, false)
	if rec := do(h, "/monitor.html", nil); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("before load: %d", rec.Code)
	}
	h, _ = newProxy(t, true)
	if rec := do(h, "/monitor.html", nil); rec.Code != http.StatusOK || rec.Body.String() != "OK" {
		t.Fatalf("after load: %d %q", rec.Code, rec.Body.String())
	}
}

func TestRedirects(t *testing.T) {
	h, _ := newProxy(t, true)
	cases := []struct{ path, want string }{
		{"/10steps", "/train-and-grow/10-basic-steps.html"},
		{"/10STEPS", "/train-and-grow/10-basic-steps.html"},
		{"//10steps", "/train-and-grow/10-basic-steps.html"},
		{"/10steps?utm_source=a", "/train-and-grow/10-basic-steps.html"},
		{"/Campus/Mixed", "/communities/campus/Mixed"},
		{"/fund?utm_source=a&e=9", "https://x.example/e/?e=31709&utm_source=a"},
	}
	for _, c := range cases {
		rec := do(h, c.path, nil)
		if rec.Code != http.StatusMovedPermanently || rec.Header().Get("Location") != c.want {
			t.Errorf("%s: %d %q, want 301 %q", c.path, rec.Code, rec.Header().Get("Location"), c.want)
		}
	}
}

func TestAEMRequest(t *testing.T) {
	h, aem := newProxy(t, true)
	s := decode(
		t,
		do(h, "/us/en.html?a=1", map[string]string{"X-Forwarded-For": "203.0.113.9", "X-Forwarded-Proto": "https"}),
	)
	aemHost := strings.TrimPrefix(aem.URL, "https://")
	switch {
	case s.Name != "aem":
		t.Fatalf("upstream %q", s.Name)
	case s.Host != aemHost:
		t.Errorf("Host %q, want origin %q", s.Host, aemHost)
	case s.EdgeKey != "k":
		t.Errorf("edge key %q", s.EdgeKey)
	case s.Path != "/us/en.html?a=1":
		t.Errorf("path %q", s.Path)
	case s.XFF != "203.0.113.9, 192.168.48.1":
		t.Errorf("XFF %q", s.XFF)
	case s.XFP != "https" || s.XFH != "www.cru.org":
		t.Errorf("XFP %q XFH %q", s.XFP, s.XFH)
	case s.XRI != "192.168.48.1":
		t.Errorf("X-Real-IP %q (peer is not a trusted proxy)", s.XRI)
	}
}

func TestVIPRequest(t *testing.T) {
	h, _ := newProxy(t, true)
	s := decode(t, do(h, "/wp-admin/x", map[string]string{"X-AEM-Edge-Key": "spoofed"}))
	switch {
	case s.Name != "vip":
		t.Fatalf("upstream %q", s.Name)
	case s.Host != "www.cru.org":
		t.Errorf("Host %q, want the visitor's host", s.Host)
	case s.EdgeKey == "k":
		t.Error("AEM edge key sent to WordPress")
	}
}

func TestCruNavRewrite(t *testing.T) {
	h, _ := newProxy(t, true)
	if s := decode(t, do(h, "/cru-nav.js", nil)); s.Path != "/cru-nav.json" {
		t.Fatalf("path %q", s.Path)
	}
}

func TestRawPathPassedThrough(t *testing.T) {
	h, _ := newProxy(t, true)
	if s := decode(t, do(h, "/a//b/../c", nil)); s.Path != "/a//b/../c" {
		t.Fatalf("path %q", s.Path)
	}
}

type hangingSource struct{}

func (hangingSource) Fetch(ctx context.Context, _ string) ([]byte, string, error) {
	<-ctx.Done()
	return nil, "", ctx.Err()
}

func TestPurgeReloadIsBounded(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := &proxy.Handler{
		Store:        store.New(hangingSource{}, 0, nil),
		Upstreams:    map[string]http.Handler{store.DefaultUpstream: http.NotFoundHandler()},
		HealthPath:   "/monitor.html",
		Logger:       logger,
		PurgeTimeout: 50 * time.Millisecond,
	}
	start := time.Now()
	do(h, "/x?purge_vanity=1", nil)
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("purge against a hung source took %s", elapsed)
	}
}

func TestUpstreamDown(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	rp, err := proxy.NewUpstream(proxy.UpstreamConfig{Name: "down", URL: "https://127.0.0.1:1"}, nil, logger)
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	rp.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusBadGateway || !strings.Contains(rec.Body.String(), "Something went wrong") {
		t.Fatalf("%d %q", rec.Code, rec.Body.String())
	}
}
