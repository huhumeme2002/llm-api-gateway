package provider

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"llmgw/internal/config"
)

type CredentialInfo struct {
	ID        string `json:"id"`
	HasProxy  bool   `json:"has_proxy"`
	ProxyKind string `json:"proxy_kind"`
}

type Slot struct {
	Info   CredentialInfo
	Client *HTTPClient
	Active atomic.Int64
}

type Pool struct {
	mu    sync.RWMutex
	order []string
	byID  map[string]*Slot
}

func NewPool(cfg config.ProviderConfig, fallbackKey string, extra map[string]string) *Pool {
	p := &Pool{byID: map[string]*Slot{}}
	creds := cfg.ResolvedCredentials()
	seen := map[string]int{}
	for _, c := range creds {
		id := strings.TrimSpace(c.ID)
		if id == "" {
			id = "default"
		}
		if n := seen[id]; n > 0 {
			id = fmt.Sprintf("%s-%d", id, n+1)
		}
		seen[c.ID]++
		key := config.LookupEnv(c.APIKeyEnv)
		if key == "" && c.APIKeyEnv == "OPENCODE_GO_API_KEY" {
			key = config.LookupEnv("OPENCODE_API_KEY")
		}
		if key == "" && fallbackKey != "" && (id == "default" || len(creds) == 1) {
			key = fallbackKey
		}
		proxy := config.LookupEnv(c.ProxyEnv)
		cli := NewHTTP(HTTPConfig{
			Name:     cfg.Name,
			BaseURL:  strings.TrimRight(cfg.BaseURL, "/"),
			APIKey:   key,
			Timeout:  cfg.Timeout,
			Extra:    extra,
			ProxyURL: proxy,
			CredID:   id,
		})
		kind := "none"
		if proxy != "" {
			kind, _ = SanitizeProxyMeta(proxy)
		}
		p.order = append(p.order, id)
		p.byID[id] = &Slot{
			Info:   CredentialInfo{ID: id, HasProxy: proxy != "", ProxyKind: kind},
			Client: cli,
		}
	}
	return p
}

func (p *Pool) IDs() []string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	out := make([]string, len(p.order))
	copy(out, p.order)
	return out
}

func (p *Pool) Infos() []CredentialInfo {
	p.mu.RLock()
	defer p.mu.RUnlock()
	out := make([]CredentialInfo, 0, len(p.order))
	for _, id := range p.order {
		out = append(out, p.byID[id].Info)
	}
	return out
}

func (p *Pool) Get(id string) *Slot {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if id == "" {
		if len(p.order) == 0 {
			return nil
		}
		return p.byID[p.order[0]]
	}
	return p.byID[id]
}

func (p *Pool) First() *Slot {
	return p.Get("")
}

func (s *Slot) Begin() { s.Active.Add(1) }
func (s *Slot) End()   { s.Active.Add(-1) }

// HealthAny hits /models on the first slot that succeeds.
func (p *Pool) HealthAny(ctx context.Context, name string) Health {
	h := Health{Name: name, CheckedAt: time.Now().UTC()}
	for _, id := range p.IDs() {
		s := p.Get(id)
		if s == nil {
			continue
		}
		_, status, _, err := s.Client.GetJSON(ctx, "models")
		if err == nil && status < 300 {
			h.Healthy = true
			return h
		}
		if err != nil {
			h.Message = Redact(err.Error())
		}
	}
	h.Healthy = false
	if h.Message == "" {
		h.Message = "no healthy credential"
	}
	return h
}

func (p *Pool) HealthOne(ctx context.Context, name, credID string) Health {
	h := Health{Name: name + "/" + credID, CheckedAt: time.Now().UTC()}
	s := p.Get(credID)
	if s == nil {
		h.Message = "unknown credential"
		return h
	}
	_, status, _, err := s.Client.GetJSON(ctx, "models")
	if err != nil {
		h.Message = Redact(err.Error())
		return h
	}
	h.Healthy = status < 300
	return h
}
