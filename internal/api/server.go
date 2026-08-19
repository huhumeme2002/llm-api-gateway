package api

import (
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func (g *Gateway) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", g.handleHealth)
	mux.HandleFunc("GET /ready", g.handleReady)
	mux.HandleFunc("GET /metrics", g.handleMetrics)

	mux.HandleFunc("GET /v1/models", g.handleModels)
	mux.HandleFunc("POST /v1/chat/completions", g.handleChat)
	mux.HandleFunc("POST /v1/responses", g.handleResponses)
	mux.HandleFunc("POST /v1/messages", g.handleMessages)

	mux.HandleFunc("GET /admin/cache/stats", g.handleAdminCacheStats)
	mux.HandleFunc("DELETE /admin/cache", g.handleAdminCacheDelete)
	mux.HandleFunc("DELETE /admin/cache/{key}", g.handleAdminCacheDelete)
	mux.HandleFunc("GET /admin/providers", g.handleAdminProviders)
	mux.HandleFunc("GET /admin/models", g.handleAdminModels)
	mux.HandleFunc("GET /admin/usage", g.handleAdminUsage)
	mux.HandleFunc("GET /admin/credentials", g.handleAdminCredentials)
	return g.withLimits(withRecover(mux))
}

func (g *Gateway) handleMetrics(w http.ResponseWriter, r *http.Request) {
	if g.Cfg != nil && !g.Cfg.Server.MetricsPublic {
		if !g.requireAdmin(w, r) {
			return
		}
	}
	promhttp.Handler().ServeHTTP(w, r)
}

func (g *Gateway) Server() *http.Server {
	maxHdr := 16 << 10
	if g.Cfg != nil && g.Cfg.Server.MaxHeaderBytes > 0 {
		maxHdr = g.Cfg.Server.MaxHeaderBytes
	}
	return &http.Server{
		Addr:              g.Cfg.Addr(),
		Handler:           g.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       g.Cfg.Server.ReadTimeout,
		WriteTimeout:      g.Cfg.Server.WriteTimeout,
		IdleTimeout:       g.Cfg.Server.IdleTimeout,
		MaxHeaderBytes:    maxHdr,
	}
}

func (g *Gateway) withLimits(next http.Handler) http.Handler {
	maxBody := int64(16 << 20)
	if g.Cfg != nil && g.Cfg.Server.MaxBodyBytes > 0 {
		maxBody = g.Cfg.Server.MaxBodyBytes
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body != nil && r.Method != http.MethodGet && r.Method != http.MethodHead {
			r.Body = http.MaxBytesReader(w, r.Body, maxBody)
		}
		w.Header().Set("X-Content-Type-Options", "nosniff")
		next.ServeHTTP(w, r)
	})
}

func withRecover(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				http.Error(w, `{"error":{"message":"internal error","type":"server_error"}}`, http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}
