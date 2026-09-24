// Command cruproxy is the proxy behind www.cru.org.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/CruGlobal/cruorg_proxy/internal/proxy"
	"github.com/CruGlobal/cruorg_proxy/internal/store"
)

const (
	loadTimeout        = 5 * time.Second
	retryWait          = 2 * time.Second
	shutdownTimeout    = 20 * time.Second
	healthcheckTimeout = 3 * time.Second
	readHeaderTimeout  = 10 * time.Second
	readTimeout        = 10 * time.Minute
	// Longer than the ALB's 60s idle timeout, so the ALB closes idle
	// connections first and never reuses one the proxy just dropped.
	idleTimeout = 75 * time.Second
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		os.Exit(healthcheck())
	}
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "cruproxy:", err)
		os.Exit(1)
	}
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func duration(key, fallback string) (time.Duration, error) {
	d, err := time.ParseDuration(env(key, fallback))
	if err != nil {
		return 0, fmt.Errorf("%s: %w", key, err)
	}
	if d <= 0 {
		return 0, fmt.Errorf("%s: must be positive, got %s", key, d)
	}
	return d, nil
}

// healthcheck backs the image's HEALTHCHECK, since the runtime image has no
// shell or curl.
func healthcheck() int {
	addr := env("LISTEN_ADDR", ":80")
	if strings.HasPrefix(addr, ":") {
		addr = "127.0.0.1" + addr
	}
	ctx, cancel := context.WithTimeout(context.Background(), healthcheckTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+addr+env("HEALTH_PATH", "/monitor.html"), nil)
	if err != nil {
		return 1
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 1
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 1
	}
	return 0
}

func newLogger() *slog.Logger {
	level := slog.LevelInfo
	_ = level.UnmarshalText([]byte(env("LOG_LEVEL", "INFO")))
	return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level}))
}

func newSource(ctx context.Context) (store.Source, string, error) {
	if path := os.Getenv("RULES_FILE"); path != "" {
		return store.FileSource{Path: path}, "file://" + path, nil
	}
	bucket, key := os.Getenv("RULES_BUCKET"), env("RULES_KEY", "rules.json")
	if bucket == "" {
		return nil, "", errors.New("set RULES_BUCKET (or RULES_FILE for local runs)")
	}
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(env("AWS_REGION", "us-east-1")))
	if err != nil {
		return nil, "", fmt.Errorf("aws config: %w", err)
	}
	return store.S3Source{Client: s3.NewFromConfig(cfg), Bucket: bucket, Key: key}, "s3://" + bucket + "/" + key, nil
}

func upstreams(trusted proxy.TrustedProxies, logger *slog.Logger) (map[string]http.Handler, error) {
	aem := os.Getenv("DEFAULT_PROXY_TARGET")
	if aem == "" {
		return nil, errors.New("DEFAULT_PROXY_TARGET is required")
	}
	aemURL, err := url.Parse(aem)
	if err != nil {
		return nil, fmt.Errorf("DEFAULT_PROXY_TARGET: %w", err)
	}
	headers := map[string]string{}
	if key := os.Getenv("AEM_EDGE_KEY"); key != "" {
		headers["X-AEM-Edge-Key"] = key
	}
	cfgs := []proxy.UpstreamConfig{{
		Name:       store.DefaultUpstream,
		URL:        aem,
		OriginHost: strings.Contains(aemURL.Hostname(), "adobeaemcloud.com"),
		Headers:    headers,
	}}
	if vip := os.Getenv("VIP_ADDR"); vip != "" {
		cfgs = append(cfgs, proxy.UpstreamConfig{Name: "VIP_ADDR", URL: vip})
	}
	out := make(map[string]http.Handler, len(cfgs))
	for _, c := range cfgs {
		rp, upErr := proxy.NewUpstream(c, trusted, logger)
		if upErr != nil {
			return nil, upErr
		}
		out[c.Name] = rp
	}
	return out, nil
}

// rulesLoader loads the rules at startup and on a timer, retrying quickly until
// the first load succeeds.
type rulesLoader struct {
	store   *store.Store
	where   string
	refresh time.Duration
	metrics *proxy.Metrics
	logger  *slog.Logger
}

func (l *rulesLoader) load(ctx context.Context) {
	lctx, cancel := context.WithTimeout(ctx, loadTimeout)
	defer cancel()
	_, _ = l.store.Load(lctx)
}

// report is the store's OnLoad hook, so timed loads and purge reloads log and
// count the same way.
func (l *rulesLoader) report(changed bool, err error, s *store.Snapshot) {
	l.metrics.LoadResult(changed, err, s)
	ctx := context.Background()
	switch {
	case err != nil:
		l.logger.ErrorContext(ctx, "rules load failed, keeping last good copy",
			slog.String("source", l.where), slog.Any("error", err))
	case changed:
		l.logger.InfoContext(ctx, "rules loaded", slog.String("source", l.where),
			slog.Int("vanities", len(s.Vanities)), slog.Int("rewrites", len(s.Rewrites)),
			slog.Int("upstreams", len(s.Upstreams)), slog.Int("forward_query", len(s.ForwardQuery)))
	}
}

func (l *rulesLoader) loop(ctx context.Context) {
	for {
		wait := l.refresh
		if l.store.Get() == nil {
			wait = retryWait
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
			l.load(ctx)
		}
	}
}

type settings struct {
	refresh, minReload time.Duration
	trusted            proxy.TrustedProxies
	healthPath         string
}

func loadSettings() (settings, error) {
	var s settings
	var err error
	if s.refresh, err = duration("RULES_REFRESH", "60s"); err != nil {
		return s, err
	}
	if s.minReload, err = duration("RULES_MIN_RELOAD", "10s"); err != nil {
		return s, err
	}
	if s.trusted, err = proxy.ParseTrusted(env("TRUSTED_PROXIES", "10.16.0.0/16")); err != nil {
		return s, fmt.Errorf("TRUSTED_PROXIES: %w", err)
	}
	s.healthPath = env("HEALTH_PATH", "/monitor.html")
	return s, nil
}

func run() error {
	logger := newLogger()
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cfg, err := loadSettings()
	if err != nil {
		return err
	}
	src, where, err := newSource(ctx)
	if err != nil {
		return err
	}
	ups, err := upstreams(cfg.trusted, logger)
	if err != nil {
		return err
	}

	metrics := proxy.NewMetrics()
	rules := store.New(src, cfg.minReload, func(p string, perr error) {
		logger.Error("bad pattern", slog.String("pattern", p), slog.Any("error", perr))
	})
	loader := &rulesLoader{store: rules, where: where, refresh: cfg.refresh, metrics: metrics, logger: logger}
	rules.OnLoad(loader.report)
	loader.load(ctx)
	go loader.loop(ctx)

	handler := &proxy.Handler{Store: rules, Upstreams: ups, HealthPath: cfg.healthPath, Logger: logger}
	srv := &http.Server{
		Addr:              env("LISTEN_ADDR", ":80"),
		Handler:           proxy.Middleware(handler, logger, metrics, cfg.trusted, cfg.healthPath),
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		IdleTimeout:       idleTimeout,
		ErrorLog:          slog.NewLogLogger(logger.Handler(), slog.LevelWarn),
	}
	metricsSrv := &http.Server{
		Addr:              env("METRICS_ADDR", ":6000"),
		Handler:           metrics.Handler(),
		ReadHeaderTimeout: readHeaderTimeout,
	}
	return serve(ctx, logger, where, srv, metricsSrv)
}

func serve(ctx context.Context, logger *slog.Logger, where string, servers ...*http.Server) error {
	errc := make(chan error, len(servers))
	for _, s := range servers {
		go func() { errc <- s.ListenAndServe() }()
	}
	logger.InfoContext(ctx, "listening", slog.String("addr", servers[0].Addr),
		slog.String("metrics", servers[len(servers)-1].Addr), slog.String("rules", where))

	var runErr error
	select {
	case runErr = <-errc:
		if errors.Is(runErr, http.ErrServerClosed) {
			runErr = nil
		}
	case <-ctx.Done():
	}
	sctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	for _, s := range servers {
		_ = s.Shutdown(sctx)
	}
	return runErr
}
