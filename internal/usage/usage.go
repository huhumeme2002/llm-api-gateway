package usage

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"llmgw/internal/protocol"
)

type Key struct {
	Tenant   string
	Provider string
}

type Counters struct {
	Requests           int64   `json:"requests"`
	InputTokens        int64   `json:"input_tokens"`
	CachedInputTokens  int64   `json:"cached_input_tokens"`
	OutputTokens       int64   `json:"output_tokens"`
	ReasoningTokens    int64   `json:"reasoning_tokens"`
	EstimatedCost      float64 `json:"estimated_cost"`
	RateLimited        int64   `json:"429"`
	Errors             int64   `json:"errors"`
	LatencyMSSum       int64   `json:"latency_ms_sum"`
	CacheHits          int64   `json:"cache_hits"`
}

type Store struct {
	mu   sync.Mutex
	mem  map[string]*Counters
	rdb  *redis.Client
}

func New(rdb *redis.Client) *Store {
	return &Store{mem: map[string]*Counters{}, rdb: rdb}
}

func (s *Store) Add(ctx context.Context, tenant, provider string, u protocol.Usage, cost float64, latency time.Duration, status int, cacheHit bool) {
	s.AddCred(ctx, tenant, provider, "", u, cost, latency, status, cacheHit)
}

func (s *Store) AddCred(ctx context.Context, tenant, provider, cred string, u protocol.Usage, cost float64, latency time.Duration, status int, cacheHit bool) {
	id := tenant + "|" + provider
	if cred != "" {
		id += "|" + cred
	}
	s.mu.Lock()
	c := s.mem[id]
	if c == nil {
		c = &Counters{}
		s.mem[id] = c
	}
	c.Requests++
	c.InputTokens += int64(u.InputTokens)
	c.CachedInputTokens += int64(u.CachedInputTokens)
	c.OutputTokens += int64(u.OutputTokens)
	c.ReasoningTokens += int64(u.ReasoningTokens)
	c.EstimatedCost += cost
	c.LatencyMSSum += latency.Milliseconds()
	if status == 429 {
		c.RateLimited++
	}
	if status >= 500 {
		c.Errors++
	}
	if cacheHit {
		c.CacheHits++
	}
	cp := *c
	s.mu.Unlock()

	if s.rdb == nil {
		return
	}
	day := time.Now().UTC().Format("2006-01-02")
	rk := "llmgw:usage:" + tenant + ":" + provider
	if cred != "" {
		rk += ":" + cred
	}
	rk += ":" + day
	pipe := s.rdb.TxPipeline()
	pipe.HIncrBy(ctx, rk, "requests", 1)
	pipe.HIncrBy(ctx, rk, "input_tokens", int64(u.InputTokens))
	pipe.HIncrBy(ctx, rk, "cached_input_tokens", int64(u.CachedInputTokens))
	pipe.HIncrBy(ctx, rk, "output_tokens", int64(u.OutputTokens))
	pipe.Expire(ctx, rk, 40*24*time.Hour)
	_, _ = pipe.Exec(ctx)
	_ = cp
}

func (s *Store) Snapshot() map[string]Counters {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]Counters, len(s.mem))
	for k, v := range s.mem {
		out[k] = *v
	}
	return out
}

func (s *Store) Provider(provider string) Counters {
	return s.Credential(provider, "")
}

func (s *Store) Credential(provider, cred string) Counters {
	s.mu.Lock()
	defer s.mu.Unlock()
	var acc Counters
	for k, v := range s.mem {
		parts := splitUsageKey(k)
		if len(parts) < 2 || parts[1] != provider {
			continue
		}
		if cred != "" && (len(parts) < 3 || parts[2] != cred) {
			continue
		}
		acc.Requests += v.Requests
		acc.InputTokens += v.InputTokens
		acc.CachedInputTokens += v.CachedInputTokens
		acc.OutputTokens += v.OutputTokens
		acc.RateLimited += v.RateLimited
		acc.Errors += v.Errors
		acc.LatencyMSSum += v.LatencyMSSum
		acc.CacheHits += v.CacheHits
		acc.EstimatedCost += v.EstimatedCost
	}
	return acc
}

func splitUsageKey(k string) []string {
	return strings.Split(k, "|")
}

func (s *Store) ErrorRate(provider string) float64 {
	c := s.Provider(provider)
	if c.Requests == 0 {
		return 0
	}
	return float64(c.Errors+c.RateLimited) / float64(c.Requests)
}

func (s *Store) AvgLatencyMS(provider string) float64 {
	c := s.Provider(provider)
	if c.Requests == 0 {
		return 0
	}
	return float64(c.LatencyMSSum) / float64(c.Requests)
}

func Ftoa(f float64) string { return strconv.FormatFloat(f, 'f', 6, 64) }
