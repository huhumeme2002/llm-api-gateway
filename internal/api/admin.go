package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"llmgw/internal/protocol"

)

func (g *Gateway) requireAdmin(w http.ResponseWriter, r *http.Request) bool {
	key := bearerOrX(r)
	if g.Admin != "" && key == g.Admin {
		return true
	}
	t, code, _ := g.Auth.Authenticate(r)
	if code == 0 && t != nil && t.Admin {
		return true
	}
	writeError(w, protocol.ChatCompletions, http.StatusUnauthorized, "authentication_error", "admin key required")
	return false
}

func bearerOrX(r *http.Request) string {
	if h := r.Header.Get("Authorization"); strings.HasPrefix(strings.ToLower(h), "bearer ") {
		return strings.TrimSpace(h[7:])
	}
	return r.Header.Get("x-api-key")
}

func (g *Gateway) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, 200, map[string]any{"status": "ok"})
}

func (g *Gateway) handleReady(w http.ResponseWriter, _ *http.Request) {
	if g.Ready != nil {
		if err := g.Ready(); err != nil {
			writeJSON(w, 503, map[string]any{"status": "not_ready", "error": err.Error()})
			return
		}
	}
	writeJSON(w, 200, map[string]any{"status": "ready"})
}

func (g *Gateway) handleAdminCacheStats(w http.ResponseWriter, r *http.Request) {
	if !g.requireAdmin(w, r) {
		return
	}
	writeJSON(w, 200, g.Cache.Stats.Snapshot())
}

func (g *Gateway) handleAdminCacheDelete(w http.ResponseWriter, r *http.Request) {
	if !g.requireAdmin(w, r) {
		return
	}
	key := r.PathValue("key")
	if key == "" {
		key = strings.TrimPrefix(strings.TrimPrefix(r.URL.Path, "/admin/cache"), "/")
	}
	if key == "" {
		ns := r.URL.Query().Get("namespace")
		if err := g.Cache.Flush(r.Context(), g.Cfg.Cache.SchemaVersion, ns); err != nil {
			writeJSON(w, 500, map[string]any{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"flushed": true, "namespace": ns})
		return
	}
	if err := g.Cache.Delete(r.Context(), key); err != nil {
		writeJSON(w, 500, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]any{"deleted": key})
}

func (g *Gateway) handleAdminProviders(w http.ResponseWriter, r *http.Request) {
	if !g.requireAdmin(w, r) {
		return
	}
	out := []map[string]any{}
	for name, p := range g.Reg.All() {
		h := p.Health(r.Context())
		u := g.Usage.Provider(name)
		avg := 0.0
		if u.Requests > 0 {
			avg = float64(u.LatencyMSSum) / float64(u.Requests)
		}
		hit := 0.0
		if u.Requests > 0 {
			hit = float64(u.CacheHits) / float64(u.Requests)
		}
		out = append(out, map[string]any{
			"name":              name,
			"health":            h,
			"recent_latency_ms": avg,
			"error_rate":        g.Usage.ErrorRate(name),
			"429_count":         u.RateLimited,
			"request_count":     u.Requests,
			"token_usage": map[string]any{
				"input": u.InputTokens, "cached_input": u.CachedInputTokens, "output": u.OutputTokens,
			},
			"cache_effectiveness": hit,
			"circuit_breaker":     g.Router.CircuitState(name),
		})
	}
	writeJSON(w, 200, map[string]any{"providers": out, "checked_at": time.Now().UTC()})
}

func (g *Gateway) handleAdminModels(w http.ResponseWriter, r *http.Request) {
	if !g.requireAdmin(w, r) {
		return
	}
	writeJSON(w, 200, map[string]any{"models": g.Reg.Models()})
}

func (g *Gateway) handleAdminCredentials(w http.ResponseWriter, r *http.Request) {
	if !g.requireAdmin(w, r) {
		return
	}
	var out []map[string]any
	for name, p := range g.Reg.All() {
		for _, info := range p.ListCredentials() {
			h := p.HealthCredential(r.Context(), info.ID)
			u := g.Usage.Credential(name, info.ID)
			avg := 0.0
			if u.Requests > 0 {
				avg = float64(u.LatencyMSSum) / float64(u.Requests)
			}
			out = append(out, map[string]any{
				"provider":          name,
				"id":                info.ID,
				"has_proxy":         info.HasProxy,
				"proxy_kind":        info.ProxyKind,
				"health":            map[string]any{"healthy": h.Healthy, "checked_at": h.CheckedAt},
				"circuit_breaker":   g.Router.CircuitStateCred(name, info.ID),
				"requests":          u.Requests,
				"active_requests":   p.ActiveRequests(info.ID),
				"input_tokens":      u.InputTokens,
				"output_tokens":     u.OutputTokens,
				"429":               u.RateLimited,
				"errors":            u.Errors,
				"recent_latency_ms": avg,
			})
		}
	}
	writeJSON(w, 200, map[string]any{"credentials": out})
}

func (g *Gateway) handleAdminUsage(w http.ResponseWriter, r *http.Request) {
	if !g.requireAdmin(w, r) {
		return
	}
	writeJSON(w, 200, g.Usage.Snapshot())
}

func (g *Gateway) handleAdminJSON(w http.ResponseWriter, status int, v any) {
	b, _ := json.Marshal(v)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(b)
}


