// Package proxy is the HTTP side of cruorg_proxy: health, redirects, and the
// reverse proxy to AEM or WordPress VIP.
package proxy

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/CruGlobal/cruorg_proxy/internal/store"
)

// DefaultPurgeTimeout bounds a purge-triggered reload. The reload holds the
// store lock, so a slow source must not stall other purges or the refresh.
const DefaultPurgeTimeout = 5 * time.Second

// Handler serves every request the ALB forwards.
type Handler struct {
	Store        *store.Store
	Upstreams    map[string]http.Handler
	HealthPath   string
	Logger       *slog.Logger
	PurgeTimeout time.Duration
}

func (h *Handler) purge(r *http.Request) {
	timeout := h.PurgeTimeout
	if timeout <= 0 {
		timeout = DefaultPurgeTimeout
	}
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()
	if _, err := h.Store.Reload(ctx); err != nil {
		h.Logger.ErrorContext(ctx, "purge reload failed", slog.Any("error", err))
	}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == h.HealthPath {
		w.Header().Set("Content-Type", "text/plain")
		if h.Store.Get() == nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("rules not loaded"))
			return
		}
		_, _ = w.Write([]byte("OK"))
		return
	}

	if r.URL.Path == "/cru-nav.js" {
		r.URL.Path, r.URL.RawPath = "/cru-nav.json", ""
	}

	query := r.URL.Query()
	if query.Has("purge_vanity") || query.Has("purge_target") {
		h.purge(r)
	}

	info := requestInfo(r)
	snap := h.Store.Get()
	name := store.DefaultUpstream
	if snap != nil {
		path := NormalizePath(r.URL.Path)
		lower := strings.ToLower(path)
		if target, key, ok := snap.Redirect(path, lower); ok {
			if snap.ForwardQuery[key] {
				target = MergeQuery(target, r.URL.RawQuery)
			}
			info.redirect = target
			w.Header().Set("Location", target)
			w.WriteHeader(http.StatusMovedPermanently)
			return
		}
		name = snap.Upstream(lower)
	}

	up, ok := h.Upstreams[name]
	if !ok {
		h.Logger.WarnContext(r.Context(), "unknown upstream, using default", slog.String("upstream", name))
		name = store.DefaultUpstream
		up = h.Upstreams[name]
	}
	info.upstream = name
	up.ServeHTTP(w, r)
}
