package proxy_test

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/CruGlobal/cruorg_proxy/internal/proxy"
	"github.com/CruGlobal/cruorg_proxy/internal/store"
)

// lockedBuffer collects log output written from server goroutines.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

// requestLog returns the fields of the access log line for path.
func (b *lockedBuffer) requestLog(t *testing.T, path string) map[string]any {
	t.Helper()
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, line := range strings.Split(b.buf.String(), "\n") {
		var rec map[string]any
		if json.Unmarshal([]byte(line), &rec) != nil || rec["msg"] != "request" {
			continue
		}
		if h, ok := rec["http"].(map[string]any); ok && h["url"] == path {
			return rec
		}
	}
	t.Fatalf("no request log for %s in:\n%s", path, b.buf.String())
	return nil
}

func statusOf(rec map[string]any) int {
	h, _ := rec["http"].(map[string]any)
	f, _ := h["status_code"].(float64)
	return int(f)
}

// stack serves the full middleware + handler in front of one TLS upstream.
func stack(t *testing.T, upstream http.HandlerFunc) (*httptest.Server, *lockedBuffer) {
	t.Helper()
	up := httptest.NewUnstartedServer(upstream)
	up.TLS = &tls.Config{MinVersion: tls.VersionTLS12}
	up.StartTLS()
	t.Cleanup(up.Close)

	logs := &lockedBuffer{}
	logger := slog.New(slog.NewJSONHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	trusted, _ := proxy.ParseTrusted("10.16.0.0/16")
	rp, err := proxy.NewUpstream(proxy.UpstreamConfig{
		Name: "aem", URL: up.URL, OriginHost: true,
		RootCAs: up.Client().Transport.(*http.Transport).TLSClientConfig.RootCAs,
	}, trusted, logger)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(store.Document{
		Vanities:  map[string]string{"/v": "/w"},
		Rewrites:  map[string]string{"^/r/(.*)": "/x/$1"},
		Upstreams: map[string]string{"^/wp-": "VIP_ADDR"},
	})
	st := store.New(memSource{body: body}, 0, nil)
	if _, loadErr := st.Load(context.Background()); loadErr != nil {
		t.Fatal(loadErr)
	}
	h := &proxy.Handler{
		Store: st, Upstreams: map[string]http.Handler{store.DefaultUpstream: rp},
		HealthPath: "/monitor.html", Logger: logger,
	}
	srv := httptest.NewServer(proxy.Middleware(h, logger, proxy.NewMetrics(), trusted, "/monitor.html"))
	t.Cleanup(srv.Close)
	return srv, logs
}

func TestClientCancelIs499(t *testing.T) {
	srv, logs := stack(t, func(_ http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(2 * time.Second):
		case <-r.Context().Done():
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/slow", nil)
	if resp, err := http.DefaultClient.Do(req); err == nil {
		_ = resp.Body.Close()
	}
	var rec map[string]any
	for range 50 {
		time.Sleep(20 * time.Millisecond)
		if strings.Contains(
			func() string { logs.mu.Lock(); defer logs.mu.Unlock(); return logs.buf.String() }(),
			`"url":"/slow"`,
		) {
			rec = logs.requestLog(t, "/slow")
			break
		}
	}
	if rec == nil {
		t.Fatal("no request log for a cancelled request")
	}
	if got := statusOf(rec); got != proxy.StatusClientClosedRequest {
		t.Fatalf("status %d, want 499", got)
	}
	if strings.Contains(logs.buf.String(), `"level":"ERROR"`) {
		t.Fatalf("client cancel logged at ERROR:\n%s", logs.buf.String())
	}
}

func TestAbortedBodyIsStillLogged(t *testing.T) {
	srv, logs := stack(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", "100000")
		_, _ = w.Write([]byte("partial"))
		if hj, ok := w.(http.Hijacker); ok {
			conn, _, _ := hj.Hijack()
			_ = conn.Close()
		}
	})
	if resp, err := http.Get(srv.URL + "/cut"); err == nil {
		_, _ = io.ReadAll(resp.Body)
		_ = resp.Body.Close()
	}
	time.Sleep(100 * time.Millisecond)
	if rec := logs.requestLog(t, "/cut"); rec["aborted"] != true {
		t.Fatalf("aborted = %v, want true", rec["aborted"])
	}
}

func TestInterimStatusNotRecorded(t *testing.T) {
	srv, logs := stack(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusEarlyHints)
		w.WriteHeader(http.StatusNotFound)
	})
	resp, err := http.Get(srv.URL + "/hints")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	time.Sleep(50 * time.Millisecond)
	if got := statusOf(logs.requestLog(t, "/hints")); got != http.StatusNotFound {
		t.Fatalf("logged %d, want 404", got)
	}
}

func TestLogKeepsOriginalURL(t *testing.T) {
	srv, logs := stack(t, func(_ http.ResponseWriter, _ *http.Request) {})
	resp, err := http.Get(srv.URL + "/cru-nav.js")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	time.Sleep(50 * time.Millisecond)
	logs.requestLog(t, "/cru-nav.js")
}
