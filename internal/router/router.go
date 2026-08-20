package router

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"llmgw/internal/config"
	"llmgw/internal/provider"
	"llmgw/internal/usage"
)

type Candidate struct {
	Provider     string
	ProviderImpl provider.Provider
	Model        string
	Weight       float64
	Alias        string
	Exact        bool
}

type Router struct {
	cfg    *config.Config
	reg    *provider.Registry
	usage  *usage.Store
	rdb    *redis.Client
	cb     map[string]*breaker
	mu     sync.Mutex
	prefix map[string]string // session -> last prefix hash
}

func New(cfg *config.Config, reg *provider.Registry, us *usage.Store, rdb *redis.Client) *Router {
	cb := map[string]*breaker{}
	for name := range cfg.Providers {
		cb[provider.NormalizeName(name)] = newBreaker(cfg.CircuitBreaker)
	}
	return &Router{cfg: cfg, reg: reg, usage: us, rdb: rdb, cb: cb, prefix: map[string]string{}}
}

func (r *Router) Resolve(model string) ([]Candidate, error) {
	model = strings.TrimSpace(model)
	if model == "" {
		model = "default"
	}
	if p, m, ok := splitProviderModel(model); ok {
		impl, found := r.reg.Get(p)
		if !found {
			return nil, fmt.Errorf("unknown provider %q", p)
		}
		up := m
		if meta, ok := r.reg.Find(impl.Name(), m); ok {
			up = meta.UpstreamID
		}
		return []Candidate{{Provider: impl.Name(), ProviderImpl: impl, Model: up, Weight: 1, Exact: true}}, nil
	}
	targets, ok := r.cfg.Aliases[model]
	if !ok {
		// bare model id: try every provider catalog
		var cs []Candidate
		for _, name := range r.reg.Names() {
			impl, _ := r.reg.Get(name)
			if meta, ok := r.reg.Find(name, model); ok {
				cs = append(cs, Candidate{Provider: impl.Name(), ProviderImpl: impl, Model: meta.UpstreamID, Weight: 1, Alias: model})
			}
		}
		if len(cs) == 0 {
			return nil, fmt.Errorf("unknown model or alias %q", model)
		}
		return cs, nil
	}
	var cs []Candidate
	for _, t := range targets {
		impl, ok := r.reg.Get(t.Provider)
		if !ok {
			continue
		}
		w := t.Weight
		if w <= 0 {
			w = 1
		}
		cs = append(cs, Candidate{Provider: impl.Name(), ProviderImpl: impl, Model: t.Model, Weight: w, Alias: model})
	}
	if len(cs) == 0 {
		return nil, fmt.Errorf("alias %q has no healthy providers", model)
	}
	return cs, nil
}

func splitProviderModel(s string) (string, string, bool) {
	// opencode-go/deepseek-v4-flash or commandcode/deepseek/deepseek-v4-flash
	for _, p := range []string{"opencode-go", "opencode_go", "opencodego", "commandcode", "command-code", "command_code", "ltnproxy", "ltn-proxy", "ltn_proxy"} {
		pref := p + "/"
		if strings.HasPrefix(strings.ToLower(s), pref) {
			return p, s[len(pref):], true
		}
	}
	if i := strings.IndexByte(s, '/'); i > 0 {
		left := s[:i]
		if _, ok := knownProvider(left); ok {
			return left, s[i+1:], true
		}
	}
	return "", "", false
}

func knownProvider(s string) (string, bool) {
	n := provider.NormalizeName(s)
	switch n {
	case "opencode_go", "opencodego", "commandcode", "command_code", "ltnproxy", "ltn_proxy":
		return n, true
	}
	return "", false
}

func (r *Router) Pick(ctx context.Context, tenant, session string, cs []Candidate) []Candidate {
	if len(cs) == 1 {
		return cs
	}
	if session != "" && r.cfg.Routing.StickySessions {
		if p, m, _, ok := r.sticky(ctx, tenant, session); ok {
			for i, c := range cs {
				if provider.NormalizeName(c.Provider) == provider.NormalizeName(p) && strings.EqualFold(c.Model, m) {
					out := append([]Candidate{c}, append(cs[:i], cs[i+1:]...)...)
					return r.filterOpen(out)
				}
			}
		}
	}
	type scored struct {
		c Candidate
		s float64
	}
	var ss []scored
	w := r.cfg.Routing.Weights
	for _, c := range cs {
		if !r.Allow(c.Provider) {
			continue
		}
		cost := 1.0 / c.Weight
		lat := r.usage.AvgLatencyMS(c.Provider)
		if lat <= 0 {
			lat = 100
		}
		errRate := r.usage.ErrorRate(c.Provider)
		quota := 0.0
		if r.usage.Provider(c.Provider).RateLimited > 0 {
			quota = 1
		}
		aff := 0.0
		if session != "" {
			if p, m, _, ok := r.sticky(ctx, tenant, session); ok && provider.NormalizeName(p) == provider.NormalizeName(c.Provider) && strings.EqualFold(m, c.Model) {
				aff = 1
			}
		}
		score := w.Cost*cost + w.Latency*(lat/1000.0) + w.Error*errRate + w.Quota*quota - w.CacheAffinity*aff
		ss = append(ss, scored{c, score})
	}
	if len(ss) == 0 {
		return r.filterOpen(cs)
	}
	// insertion sort by score
	for i := 1; i < len(ss); i++ {
		j := i
		for j > 0 && ss[j].s < ss[j-1].s {
			ss[j], ss[j-1] = ss[j-1], ss[j]
			j--
		}
	}
	out := make([]Candidate, 0, len(ss))
	for _, x := range ss {
		out = append(out, x.c)
	}
	return out
}

func (r *Router) filterOpen(cs []Candidate) []Candidate {
	var out []Candidate
	for _, c := range cs {
		if r.Allow(c.Provider) {
			out = append(out, c)
		}
	}
	if len(out) == 0 {
		return cs
	}
	return out
}

func (r *Router) Allow(name string) bool {
	r.mu.Lock()
	b := r.cb[provider.NormalizeName(name)]
	r.mu.Unlock()
	if b == nil {
		return true
	}
	return b.Allow()
}

func (r *Router) Report(name string, status int, err error) {
	r.ReportCred(name, "", status, err)
}

func (r *Router) ReportCred(name, cred string, status int, err error) {
	b := r.breaker(name, cred)
	if err != nil || status >= 500 || status == 429 {
		b.Failure()
		return
	}
	if status > 0 && status < 400 {
		b.Success()
	}
}

func (r *Router) breaker(name, cred string) *breaker {
	key := cbKey(name, cred)
	r.mu.Lock()
	defer r.mu.Unlock()
	if b := r.cb[key]; b != nil {
		return b
	}
	b := newBreaker(r.cfg.CircuitBreaker)
	r.cb[key] = b
	return b
}

func cbKey(name, cred string) string {
	n := provider.NormalizeName(name)
	if cred == "" {
		return n
	}
	return n + "/" + cred
}

func (r *Router) AllowCred(name, cred string) bool {
	return r.breaker(name, cred).Allow()
}

func (r *Router) CircuitState(name string) State {
	return r.CircuitStateCred(name, "")
}

func (r *Router) CircuitStateCred(name, cred string) State {
	return r.breaker(name, cred).State()
}

func (r *Router) RememberSticky(ctx context.Context, tenant, session, prov, model string) {
	r.RememberStickyCred(ctx, tenant, session, prov, model, "")
}

func (r *Router) RememberStickyCred(ctx context.Context, tenant, session, prov, model, cred string) {
	if session == "" || !r.cfg.Routing.StickySessions {
		return
	}
	val := prov + "|" + model
	if cred != "" {
		val += "|" + cred
	}
	if r.rdb != nil {
		_ = r.rdb.Set(ctx, stickyKey(tenant, session), val, r.cfg.Routing.StickyTTL).Err()
		return
	}
	r.mu.Lock()
	r.prefix["sticky:"+tenant+":"+session] = val
	r.mu.Unlock()
}

func (r *Router) StickyCred(ctx context.Context, tenant, session string) (prov, model, cred string, ok bool) {
	p, m, c, ok := r.sticky(ctx, tenant, session)
	return p, m, c, ok
}

func (r *Router) OrderCredentials(ctx context.Context, tenant, session, prov string, ids []string) []string {
	if len(ids) == 0 {
		return []string{"default"}
	}
	var sticky string
	if session != "" && r.cfg.Routing.StickySessions {
		if p, _, c, ok := r.sticky(ctx, tenant, session); ok && provider.NormalizeName(p) == provider.NormalizeName(prov) {
			sticky = c
		}
	}
	var out []string
	seen := map[string]bool{}
	push := func(id string) {
		if id == "" || seen[id] || !r.AllowCred(prov, id) {
			return
		}
		seen[id] = true
		out = append(out, id)
	}
	push(sticky)
	for _, id := range ids {
		push(id)
	}
	if len(out) == 0 {
		return append([]string{}, ids...)
	}
	return out
}

func (r *Router) sticky(ctx context.Context, tenant, session string) (string, string, string, bool) {
	var val string
	if r.rdb != nil {
		v, err := r.rdb.Get(ctx, stickyKey(tenant, session)).Result()
		if err != nil {
			return "", "", "", false
		}
		val = v
	} else {
		r.mu.Lock()
		val = r.prefix["sticky:"+tenant+":"+session]
		r.mu.Unlock()
	}
	parts := strings.Split(val, "|")
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return "", "", "", false
	}
	cred := ""
	if len(parts) >= 3 {
		cred = parts[2]
	}
	return parts[0], parts[1], cred, true
}

func stickyKey(tenant, session string) string {
	return "llmgw:sticky:" + tenant + ":" + session
}

func (r *Router) TrackPrefix(ctx context.Context, tenant, session, hash string, length int) (reuse float64, prev string) {
	if session == "" || hash == "" {
		return 0, ""
	}
	key := "llmgw:prefix:" + tenant + ":" + session
	if r.rdb != nil {
		prev, _ = r.rdb.Get(ctx, key).Result()
		_ = r.rdb.Set(ctx, key, hash, 24*time.Hour).Err()
	} else {
		r.mu.Lock()
		prev = r.prefix[key]
		r.prefix[key] = hash
		r.mu.Unlock()
	}
	if prev != "" && prev == hash {
		return 1, prev
	}
	if prev != "" {
		return 0, prev
	}
	return 0, ""
}
