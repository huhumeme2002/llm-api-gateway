package provider

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"
)

type Registry struct {
	mu       sync.RWMutex
	byName   map[string]Provider
	models   []Model
	byID     map[string]Model // "provider/upstream"
	log      *slog.Logger
	interval time.Duration
}

func NewRegistry(log *slog.Logger, interval time.Duration, providers ...Provider) *Registry {
	r := &Registry{
		byName:   map[string]Provider{},
		byID:     map[string]Model{},
		log:      log,
		interval: interval,
	}
	for _, p := range providers {
		if p != nil {
			r.byName[NormalizeName(p.Name())] = p
		}
	}
	return r
}

func (r *Registry) Get(name string) (Provider, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.byName[NormalizeName(name)]
	return p, ok
}

func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.byName))
	for k := range r.byName {
		out = append(out, k)
	}
	return out
}

func (r *Registry) All() map[string]Provider {
	r.mu.RLock()
	defer r.mu.RUnlock()
	cp := make(map[string]Provider, len(r.byName))
	for k, v := range r.byName {
		cp[k] = v
	}
	return cp
}

func (r *Registry) Models() []Model {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Model, len(r.models))
	copy(out, r.models)
	return out
}

func (r *Registry) Find(providerName, modelID string) (Model, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	key := NormalizeName(providerName) + "/" + modelID
	if m, ok := r.byID[key]; ok {
		return m, true
	}
	// case-insensitive model id
	for k, m := range r.byID {
		if strings.EqualFold(k, key) {
			return m, true
		}
	}
	return Model{}, false
}

func (r *Registry) Refresh(ctx context.Context) {
	var all []Model
	idx := map[string]Model{}
	for name, p := range r.All() {
		ms, err := p.ListModels(ctx)
		if err != nil {
			if r.log != nil {
				r.log.Warn("model refresh failed", "provider", name, "err", err.Error())
			}
			continue
		}
		for _, m := range ms {
			m.Provider = name
			if m.Health == "" {
				m.Health = "up"
			}
			all = append(all, m)
			idx[NormalizeName(name)+"/"+m.UpstreamID] = m
		}
	}
	r.mu.Lock()
	r.models = all
	r.byID = idx
	r.mu.Unlock()
}

func (r *Registry) Start(ctx context.Context) {
	r.Refresh(ctx)
	if r.interval <= 0 {
		return
	}
	t := time.NewTicker(r.interval)
	go func() {
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				r.Refresh(ctx)
			}
		}
	}()
}
