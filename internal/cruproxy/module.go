// Package cruproxy is the Caddy handler that redirects cru.org paths and picks
// the upstream for everything else.
package cruproxy

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
	"github.com/caddyserver/caddy/v2/caddyconfig/httpcaddyfile"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"

	"github.com/CruGlobal/cruorg_proxy/internal/store"
)

// UpstreamVar is the placeholder the Caddyfile matches on to pick a route.
const UpstreamVar = "http.cruproxy.upstream"

const (
	defaultRefresh   = 60 * time.Second
	defaultMinReload = 10 * time.Second
	loadTimeout      = 5 * time.Second
	retryWait        = 2 * time.Second
)

var (
	_ caddy.Provisioner           = (*Handler)(nil)
	_ caddy.CleanerUpper          = (*Handler)(nil)
	_ caddyhttp.MiddlewareHandler = (*Handler)(nil)
	_ caddyfile.Unmarshaler       = (*Handler)(nil)
)

func init() {
	caddy.RegisterModule(Handler{})
	httpcaddyfile.RegisterHandlerDirective(
		"cruproxy",
		func(h httpcaddyfile.Helper) (caddyhttp.MiddlewareHandler, error) {
			var m Handler
			err := m.UnmarshalCaddyfile(h.Dispenser)
			return &m, err
		},
	)
}

// Handler redirects vanity and rewrite matches and tags the rest with an
// upstream name.
type Handler struct {
	RedisAddr       string         `json:"redis_addr,omitempty"`
	RedisDB         int            `json:"redis_db,omitempty"`
	VanityKey       string         `json:"vanity_key,omitempty"`
	RewritesKey     string         `json:"rewrites_key,omitempty"`
	UpstreamsKey    string         `json:"upstreams_key,omitempty"`
	ForwardQueryKey string         `json:"forward_query_key,omitempty"`
	Refresh         caddy.Duration `json:"refresh,omitempty"`
	MinReload       caddy.Duration `json:"min_reload,omitempty"`
	HealthPath      string         `json:"health_path,omitempty"`

	store  *store.Store
	client *redis.Client
	cancel context.CancelFunc
	logger *zap.Logger
}

// CaddyModule returns the Caddy module information.
func (Handler) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{
		ID:  "http.handlers.cruproxy",
		New: func() caddy.Module { return new(Handler) },
	}
}

// zapRedisLogger routes go-redis's internal messages into Caddy's JSON logs.
type zapRedisLogger struct{ l *zap.Logger }

func (z zapRedisLogger) Printf(_ context.Context, format string, v ...any) {
	z.l.Warn(fmt.Sprintf(format, v...))
}

type redisSource struct{ c *redis.Client }

func (r redisSource) HGetAll(ctx context.Context, key string) (map[string]string, error) {
	return r.c.HGetAll(ctx, key).Result()
}

// Provision connects to Redis, does a first load, and starts the refresh loop.
// A failed first load is not fatal: the health path reports 503 until the loop
// succeeds, so ECS keeps the old tasks.
func (h *Handler) Provision(ctx caddy.Context) error {
	h.logger = ctx.Logger(h)
	redis.SetLogger(zapRedisLogger{h.logger.Named("redis")})
	if h.Refresh == 0 {
		h.Refresh = caddy.Duration(defaultRefresh)
	}
	if h.MinReload == 0 {
		h.MinReload = caddy.Duration(defaultMinReload)
	}
	if h.HealthPath == "" {
		h.HealthPath = "/monitor.html"
	}
	h.client = redis.NewClient(&redis.Options{
		Addr:         h.RedisAddr,
		DB:           h.RedisDB,
		DialTimeout:  time.Second,
		ReadTimeout:  time.Second,
		WriteTimeout: time.Second,
	})
	h.store = store.New(redisSource{h.client}, store.Keys{
		Vanities:     h.VanityKey,
		Rewrites:     h.RewritesKey,
		Upstreams:    h.UpstreamsKey,
		ForwardQuery: h.ForwardQueryKey,
	}, time.Duration(h.MinReload), func(p string, err error) {
		h.logger.Error("bad pattern", zap.String("pattern", p), zap.Error(err))
	})

	loopCtx, cancel := context.WithCancel(context.Background())
	h.cancel = cancel
	h.load(loopCtx)
	go h.loop(loopCtx)
	return nil
}

func (h *Handler) load(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, loadTimeout)
	defer cancel()
	if err := h.store.Load(ctx); err != nil {
		h.logger.Error("redis load failed, keeping last good copy", zap.Error(err))
		return
	}
	s := h.store.Get()
	h.logger.Info("rules loaded",
		zap.Int("vanities", len(s.Vanities)),
		zap.Int("rewrites", len(s.Rewrites)),
		zap.Int("upstreams", len(s.Upstreams)),
		zap.Int("forward_query", len(s.ForwardQuery)))
}

func (h *Handler) loop(ctx context.Context) {
	for {
		wait := time.Duration(h.Refresh)
		if h.store.Get() == nil {
			wait = retryWait
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
			h.load(ctx)
		}
	}
}

// Cleanup stops the refresh loop and closes the Redis client.
func (h *Handler) Cleanup() error {
	if h.cancel != nil {
		h.cancel()
	}
	if h.client != nil {
		return h.client.Close()
	}
	return nil
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request, next caddyhttp.Handler) error {
	if r.URL.Path == h.HealthPath {
		w.Header().Set("Content-Type", "text/plain")
		if h.store.Get() == nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("rules not loaded"))
			return nil
		}
		_, _ = w.Write([]byte("OK"))
		return nil
	}

	query := r.URL.Query()
	if query.Has("purge_vanity") || query.Has("purge_target") {
		if err := h.store.Reload(r.Context()); err != nil {
			h.logger.Error("purge reload failed", zap.Error(err))
		}
	}

	repl := r.Context().Value(caddy.ReplacerCtxKey).(*caddy.Replacer) //nolint:errcheck // always set by caddyhttp
	snap := h.store.Get()
	if snap == nil {
		repl.Set(UpstreamVar, store.DefaultUpstream)
		return next.ServeHTTP(w, r)
	}

	path := NormalizePath(r.URL.Path)
	lower := strings.ToLower(path)
	if target, key, ok := snap.Redirect(path, lower); ok {
		if snap.ForwardQuery[key] {
			target = MergeQuery(target, r.URL.RawQuery)
		}
		w.Header().Set("Location", target)
		w.WriteHeader(http.StatusMovedPermanently)
		return nil
	}
	repl.Set(UpstreamVar, snap.Upstream(lower))
	return next.ServeHTTP(w, r)
}

// UnmarshalCaddyfile parses:
//
//	cruproxy {
//	    redis <host:port>
//	    db <n>
//	    vanity_key <key>
//	    rewrites_key <key>
//	    upstreams_key <key>
//	    forward_query_key [<key>]
//	    refresh <duration>
//	    min_reload <duration>
//	    health_path <path>
//	}
func (h *Handler) UnmarshalCaddyfile(d *caddyfile.Dispenser) error {
	d.Next()
	for d.NextBlock(0) {
		opt := d.Val()
		args := d.RemainingArgs()
		if len(args) > 1 {
			return d.ArgErr()
		}
		val := ""
		if len(args) == 1 {
			val = args[0]
		}
		switch opt {
		case "redis":
			h.RedisAddr = val
		case "db":
			n, err := strconv.Atoi(val)
			if err != nil {
				return d.Errf("db: %v", err)
			}
			h.RedisDB = n
		case "vanity_key":
			h.VanityKey = val
		case "rewrites_key":
			h.RewritesKey = val
		case "upstreams_key":
			h.UpstreamsKey = val
		case "forward_query_key":
			h.ForwardQueryKey = val
		case "refresh", "min_reload":
			dur, err := caddy.ParseDuration(val)
			if err != nil {
				return d.Errf("%s: %v", opt, err)
			}
			if opt == "refresh" {
				h.Refresh = caddy.Duration(dur)
			} else {
				h.MinReload = caddy.Duration(dur)
			}
		case "health_path":
			h.HealthPath = val
		default:
			return d.Errf("unknown option %q", opt)
		}
	}
	return nil
}
