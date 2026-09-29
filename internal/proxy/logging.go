package proxy

import (
	"context"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

type reqInfo struct {
	upstream string
	redirect string
	// Set from the upstream transport's trace hooks, which can fire on other
	// goroutines, so they are atomic.
	upstreamConnect atomic.Int64
	upstreamHeader  atomic.Int64
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
	final  bool
}

// WriteHeader passes every code through but records only the final one; 1xx
// replies such as 103 Early Hints precede the real status.
func (w *statusWriter) WriteHeader(code int) {
	if !w.final && code >= http.StatusOK {
		w.status, w.final = code, true
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	w.final = true
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
		// Captured before the handler can rewrite r.URL (/cru-nav.js).
		requestURI := r.URL.RequestURI()
		path, rawQuery := r.URL.Path, r.URL.RawQuery
		health := r.URL.Path == healthPath
		r = r.WithContext(context.WithValue(r.Context(), ctxKey{}, info))
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}

		// Deferred so a response cut off mid-body is still logged; ReverseProxy
		// signals that by panicking with http.ErrAbortHandler, which is re-raised
		// so net/http still drops the connection.
		defer func() {
			aborted := recover()
			elapsed := time.Since(start)
			kind := "proxy"
			switch {
			case health:
				kind = "health"
			case info.redirect != "":
				kind = "redirect"
			}
			m.observe(kind, info.upstream, sw.status, elapsed)
			if !health {
				logRequest(r, logger, trusted, sw, info, requestURI, path, rawQuery, elapsed, aborted != nil)
			}
			if aborted != nil {
				panic(aborted)
			}
		}()
		next.ServeHTTP(sw, r)
	})
}

// logRequest writes the access log line. The level follows the status the way
// the nginx pipeline did: 5xx is an error, 4xx a warning.
func logRequest(
	r *http.Request,
	logger *slog.Logger,
	trusted TrustedProxies,
	sw *statusWriter,
	info *reqInfo,
	requestURI, path, rawQuery string,
	elapsed time.Duration,
	aborted bool,
) {
	level := slog.LevelInfo
	switch {
	case sw.status >= http.StatusInternalServerError:
		level = slog.LevelError
	case sw.status >= http.StatusBadRequest:
		level = slog.LevelWarn
	}
	msg := r.Method + " " + requestURI + " " + strconv.Itoa(sw.status)
	if info.redirect != "" {
		msg += " -> " + info.redirect
	}

	urlDetails := []any{slog.String("path", path)}
	if q := queryAttrs(rawQuery); len(q) > 0 {
		urlDetails = append(urlDetails, slog.Group("queryString", q...))
	}
	attrs := []slog.Attr{
		slog.Group("http",
			slog.String("method", r.Method),
			slog.String("url", requestURI),
			slog.Group("url_details", urlDetails...),
			slog.String("version", strings.TrimPrefix(r.Proto, "HTTP/")),
			slog.String("host", r.Host),
			slog.Int("status_code", sw.status),
			slog.String("useragent", r.UserAgent()),
			slog.String("referer", r.Referer()),
			// Same attribute the nginx pipeline produced. Behind CloudFront the
			// first address is the visitor.
			slog.String("_x_forwarded_for", strings.Join(r.Header.Values("X-Forwarded-For"), ", ")),
		),
		slog.Group("network",
			slog.Group("client", slog.String("ip", trusted.ClientIP(r))),
			slog.Int64("bytes_written", sw.bytes),
		),
		slog.Int64("duration", elapsed.Nanoseconds()),
		slog.String("upstream", info.upstream),
		slog.String("redirect", info.redirect),
		slog.Bool("aborted", aborted),
		slog.String("amzn_trace_id", r.Header.Get("X-Amzn-Trace-Id")),
		slog.String("viewer_country", r.Header.Get("Cloudfront-Viewer-Country")),
	}
	// nginx's uct and uht: time to a new upstream connection (absent when a
	// kept-alive one was reused) and to the upstream's first response byte.
	if d := info.upstreamConnect.Load(); d > 0 {
		attrs = append(attrs, slog.Int64("upstream_connect_duration", d))
	}
	if d := info.upstreamHeader.Load(); d > 0 {
		attrs = append(attrs, slog.Int64("upstream_header_duration", d))
	}
	logger.LogAttrs(r.Context(), level, msg, attrs...)
}

// queryAttrs mirrors Datadog's URL parser: one attribute per parameter, a list
// when the parameter repeats.
func queryAttrs(raw string) []any {
	q, _ := url.ParseQuery(raw)
	out := make([]any, 0, len(q))
	for k, v := range q {
		if len(v) == 1 {
			out = append(out, slog.String(k, v[0]))
		} else {
			out = append(out, slog.Any(k, v))
		}
	}
	return out
}
