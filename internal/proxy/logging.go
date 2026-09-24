package proxy

import (
	"context"
	"log/slog"
	"net/http"
	"time"
)

type reqInfo struct {
	upstream string
	redirect string
}

type ctxKey struct{}

// requestInfo returns the per-request record the middleware logs.
func requestInfo(r *http.Request) *reqInfo {
	if v, ok := r.Context().Value(ctxKey{}).(*reqInfo); ok {
		return v
	}
	return &reqInfo{}
}

type statusWriter struct {
	http.ResponseWriter

	status int
	bytes  int64
	wrote  bool
}

func (w *statusWriter) WriteHeader(code int) {
	if !w.wrote {
		w.status, w.wrote = code, true
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	w.wrote = true
	n, err := w.ResponseWriter.Write(b)
	w.bytes += int64(n)
	return n, err
}

// Unwrap lets http.ResponseController reach the underlying writer.
func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// Middleware logs one JSON line per request, using Datadog's standard
// attribute names, and records request metrics. Health checks are counted but
// not logged.
func Middleware(
	next http.Handler,
	logger *slog.Logger,
	m *Metrics,
	trusted TrustedProxies,
	healthPath string,
) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		info := &reqInfo{}
		r = r.WithContext(context.WithValue(r.Context(), ctxKey{}, info))
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		health := r.URL.Path == healthPath
		next.ServeHTTP(sw, r)
		elapsed := time.Since(start)

		kind := "proxy"
		switch {
		case health:
			kind = "health"
		case info.redirect != "":
			kind = "redirect"
		}
		m.observe(kind, info.upstream, sw.status, elapsed)
		if health {
			return
		}
		logger.LogAttrs(r.Context(), slog.LevelInfo, "request",
			slog.Group("http",
				slog.String("method", r.Method),
				slog.String("url", r.URL.RequestURI()),
				slog.String("host", r.Host),
				slog.Int("status_code", sw.status),
				slog.String("useragent", r.UserAgent()),
				slog.String("referer", r.Referer()),
			),
			slog.Group("network",
				slog.Group("client", slog.String("ip", trusted.ClientIP(r))),
				slog.Int64("bytes_written", sw.bytes),
			),
			slog.Int64("duration", elapsed.Nanoseconds()),
			slog.String("upstream", info.upstream),
			slog.String("redirect", info.redirect),
			slog.String("amzn_trace_id", r.Header.Get("X-Amzn-Trace-Id")),
			slog.String("viewer_country", r.Header.Get("Cloudfront-Viewer-Country")),
		)
	})
}
