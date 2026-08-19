package auth

import (
	"net/http"
	"strings"
	"sync"
	"time"

	"llmgw/internal/config"
)

type Tenant struct {
	ID                 string
	Admin              bool
	RateLimitRPM       int
	MonthlyTokenBudget int64
	AllowedProviders   []string
	AllowedModels      []string
	CacheNamespace     string
	SharedCache        bool
}

type Service struct {
	required bool
	byKey    map[string]*Tenant
	limiters sync.Map
}

func New(cfg *config.Config) *Service {
	s := &Service{required: cfg.Server.AuthRequired, byKey: map[string]*Tenant{}}
	for _, t := range cfg.Tenants {
		if t.APIKey == "" {
			continue
		}
		s.byKey[t.APIKey] = &Tenant{
			ID:                 t.ID,
			Admin:              t.Admin,
			RateLimitRPM:       t.RateLimitRPM,
			MonthlyTokenBudget: t.MonthlyTokenBudget,
			AllowedProviders:   t.AllowedProviders,
			AllowedModels:      t.AllowedModels,
			CacheNamespace:     t.CacheNamespace,
			SharedCache:        t.SharedCache,
		}
	}
	return s
}

func Bearer(r *http.Request) string {
	if h := r.Header.Get("Authorization"); strings.HasPrefix(strings.ToLower(h), "bearer ") {
		return strings.TrimSpace(h[7:])
	}
	if v := r.Header.Get("x-api-key"); v != "" {
		return v
	}
	if v := r.Header.Get("x-gateway-key"); v != "" {
		return v
	}
	return ""
}

func (s *Service) Authenticate(r *http.Request) (*Tenant, int, string) {
	key := Bearer(r)
	if key == "" {
		if !s.required {
			return &Tenant{ID: "anonymous", CacheNamespace: "anonymous"}, 0, ""
		}
		return nil, http.StatusUnauthorized, "missing api key"
	}
	t, ok := s.byKey[key]
	if !ok {
		if !s.required {
			return &Tenant{ID: "anonymous", CacheNamespace: "anonymous"}, 0, ""
		}
		return nil, http.StatusUnauthorized, "invalid api key"
	}
	if t.RateLimitRPM > 0 && !s.allow(t.ID, t.RateLimitRPM) {
		return nil, http.StatusTooManyRequests, "tenant rate limit"
	}
	return t, 0, ""
}

func (s *Service) AllowModel(t *Tenant, provider, model string) bool {
	if t == nil {
		return true
	}
	if len(t.AllowedProviders) > 0 {
		ok := false
		for _, p := range t.AllowedProviders {
			if strings.EqualFold(p, provider) {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}
	if len(t.AllowedModels) > 0 {
		for _, m := range t.AllowedModels {
			if m == model || m == provider+"/"+model || strings.EqualFold(m, model) {
				return true
			}
		}
		return false
	}
	return true
}

type win struct {
	mu    sync.Mutex
	start time.Time
	n     int
}

func (s *Service) allow(id string, rpm int) bool {
	v, _ := s.limiters.LoadOrStore(id, &win{start: time.Now()})
	w := v.(*win)
	w.mu.Lock()
	defer w.mu.Unlock()
	if time.Since(w.start) >= time.Minute {
		w.start = time.Now()
		w.n = 0
	}
	if w.n >= rpm {
		return false
	}
	w.n++
	return true
}
