package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"runtime/debug"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/redis/go-redis/v9"
	"llmgw/internal/api"
	"llmgw/internal/auth"
	"llmgw/internal/cache"
	"llmgw/internal/config"
	"llmgw/internal/cost"
	"llmgw/internal/logx"
	"llmgw/internal/metrics"
	"llmgw/internal/provider"
	"llmgw/internal/provider/commandcode"
	"llmgw/internal/provider/ltnproxy"
	"llmgw/internal/provider/opencodego"
	"llmgw/internal/router"
	"llmgw/internal/singleflight"
	"llmgw/internal/usage"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("config", "err", err)
		os.Exit(1)
	}
	applyMemoryLimit(cfg)
	log := logx.New(cfg.Server.LogPrompts)
	slog.SetDefault(log)

	rdb := connectRedis(cfg, log)

	store, err := cache.New(cfg.Cache, rdb)
	if err != nil {
		log.Error("cache", "err", err)
		os.Exit(1)
	}
	sf := singleflight.New(cfg.Cache.Singleflight, rdb)
	us := usage.New(rdb)
	m := metrics.New(prometheus.DefaultRegisterer)

	var adapters []provider.Provider
	if p, ok := cfg.Providers["opencode_go"]; ok && p.Enabled {
		if p.BaseURL == "" {
			p.BaseURL = "https://opencode.ai/zen/go/v1"
		}
		adapters = append(adapters, opencodego.New(p, cfg.ProviderAPIKey(p)))
	}
	if p, ok := cfg.Providers["commandcode"]; ok && p.Enabled {
		if p.BaseURL == "" {
			p.BaseURL = "https://api.commandcode.ai/provider/v1"
		}
		adapters = append(adapters, commandcode.New(p, cfg.ProviderAPIKey(p)))
	}
	if p, ok := cfg.Providers["ltnproxy"]; ok && p.Enabled {
		if p.BaseURL == "" {
			p.BaseURL = "https://ltnproxy.com/v1"
		}
		adapters = append(adapters, ltnproxy.New(p, cfg.ProviderAPIKey(p)))
	}
	reg := provider.NewRegistry(log, cfg.Models.RefreshInterval, adapters...)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	reg.Start(ctx)

	rt := router.New(cfg, reg, us, rdb)
	prices := cost.Load(cfg.PricingFile)

	gw := &api.Gateway{
		Cfg:    cfg,
		Log:    log,
		Auth:   auth.New(cfg),
		Cache:  store,
		SF:     sf,
		Reg:    reg,
		Router: rt,
		Usage:  us,
		Cost:   prices,
		M:      m,
		Admin:  cfg.AdminKey(),
		Ready: func() error {
			if !cfg.Redis.Enabled {
				return nil
			}
			if rdb == nil {
				if cfg.Server.RedisStrictReady {
					return errors.New("redis required but not connected")
				}
				return nil
			}
			c, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			return rdb.Ping(c).Err()
		},
	}

	srv := gw.Server()
	go func() {
		var ms runtime.MemStats
		runtime.ReadMemStats(&ms)
		log.Info("listening",
			"addr", cfg.Addr(),
			"gomaxprocs", runtime.GOMAXPROCS(0),
			"go_version", runtime.Version(),
			"heap_alloc_mb", ms.Alloc/1024/1024,
			"redis", rdb != nil,
		)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("server", "err", err)
			os.Exit(1)
		}
	}()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	log.Info("shutting down")
	cancel()
	shctx, shcancel := context.WithTimeout(context.Background(), cfg.Server.ShutdownTimeout)
	defer shcancel()
	_ = srv.Shutdown(shctx)
	if rdb != nil {
		_ = rdb.Close()
	}
}

func applyMemoryLimit(cfg *config.Config) {
	if cfg.Server.MemLimit == "" {
		return
	}
	n, err := config.ParseMemLimit(cfg.Server.MemLimit)
	if err != nil || n <= 0 {
		return
	}
	debug.SetMemoryLimit(n)
}

func connectRedis(cfg *config.Config, log *slog.Logger) *redis.Client {
	if !cfg.Redis.Enabled || cfg.Redis.URL == "" {
		return nil
	}
	opt, err := redis.ParseURL(cfg.Redis.URL)
	if err != nil {
		log.Error("redis url", "err", err)
		os.Exit(1)
	}
	opt.PoolSize = cfg.Redis.PoolSize
	opt.MinIdleConns = cfg.Redis.MinIdleConns
	opt.DialTimeout = 3 * time.Second
	opt.ReadTimeout = 3 * time.Second
	opt.WriteTimeout = 3 * time.Second
	opt.PoolTimeout = 4 * time.Second
	opt.ConnMaxIdleTime = 5 * time.Minute
	opt.MaxRetries = 2
	rdb := redis.NewClient(opt)

	const attempts = 20
	var last error
	for i := 1; i <= attempts; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		last = rdb.Ping(ctx).Err()
		cancel()
		if last == nil {
			return rdb
		}
		log.Warn("waiting for redis", "attempt", i, "err", last.Error())
		time.Sleep(500 * time.Millisecond)
	}
	log.Warn("redis unavailable; L1-only mode", "err", last)
	_ = rdb.Close()
	return nil
}
