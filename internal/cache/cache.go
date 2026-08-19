package cache

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	lru "github.com/hashicorp/golang-lru/v2"
	"github.com/redis/go-redis/v9"
	"llmgw/internal/config"
	"llmgw/internal/protocol"
)

type Mode string

const (
	ModeAuto   Mode = "auto"
	ModeBypass Mode = "bypass"
	ModeForce  Mode = "force"
)

type HitKind string

const (
	HitNone   HitKind = "MISS"
	HitL1     HitKind = "HIT-L1"
	HitL2     HitKind = "HIT-REDIS"
	HitBypass HitKind = "BYPASS"
)

type Entry struct {
	Completion protocol.Completion `json:"completion"`
	Provider   string              `json:"provider"`
	Model      string              `json:"model"`
	CreatedAt  time.Time           `json:"created_at"`
	Bytes      int                 `json:"bytes"`
}

type Stats struct {
	L1Hits                    atomic.Int64
	L2Hits                    atomic.Int64
	Misses                    atomic.Int64
	SingleflightSaved         atomic.Int64
	BytesSaved                atomic.Int64
	EstimatedInputTokensSaved atomic.Int64
	EstimatedOutputTokensSaved atomic.Int64
	EstimatedCostSaved        atomic.Int64 // micro-USD * 100 (we store cents*10000 via float separately)
	CostSaved                 atomic.Uint64
}

func (s *Stats) Snapshot() map[string]any {
	l1 := s.L1Hits.Load()
	l2 := s.L2Hits.Load()
	miss := s.Misses.Load()
	total := l1 + l2 + miss
	ratio := 0.0
	if total > 0 {
		ratio = float64(l1+l2) / float64(total)
	}
	return map[string]any{
		"l1_hits":                       l1,
		"l2_hits":                       l2,
		"misses":                        miss,
		"hit_ratio":                     ratio,
		"singleflight_saved_requests":   s.SingleflightSaved.Load(),
		"bytes_saved":                   s.BytesSaved.Load(),
		"estimated_input_tokens_saved":  s.EstimatedInputTokensSaved.Load(),
		"estimated_output_tokens_saved": s.EstimatedOutputTokensSaved.Load(),
		"estimated_cost_saved":          float64(s.CostSaved.Load()) / 1e6,
	}
}

type Store struct {
	cfg   config.CacheConfig
	l1    *lru.Cache[string, l1ent]
	rdb   *redis.Client
	Stats *Stats
	sem   Semantic
	mu    sync.Mutex
}

type l1ent struct {
	entry Entry
	exp   time.Time
}

func New(cfg config.CacheConfig, rdb *redis.Client) (*Store, error) {
	var c *lru.Cache[string, l1ent]
	var err error
	if cfg.L1.Enabled {
		c, err = lru.New[string, l1ent](cfg.L1.MaxEntries)
		if err != nil {
			return nil, err
		}
	}
	return &Store{cfg: cfg, l1: c, rdb: rdb, Stats: &Stats{}, sem: Semantic{Enabled: cfg.Semantic.Enabled, Allowed: cfg.Semantic.AllowedWorkloads}}, nil
}

func ParseMode(h string) Mode {
	switch strings.ToLower(strings.TrimSpace(h)) {
	case "bypass", "no-cache", "no_cache":
		return ModeBypass
	case "force":
		return ModeForce
	default:
		return ModeAuto
	}
}

func Eligible(mode Mode, p *protocol.ParsedRequest) bool {
	if mode == ModeBypass {
		return false
	}
	if p.PreviousResponse != "" {
		return false
	}
	if mode == ModeForce {
		return true
	}
	if p.Seed != nil {
		return true
	}
	if p.Temperature != nil && *p.Temperature == 0 {
		return true
	}
	return false
}

func (s *Store) Peek(ctx context.Context, key string) (*Entry, bool) {
	if !s.cfg.Enabled {
		return nil, false
	}
	if s.cfg.L1.Enabled && s.l1 != nil {
		if v, ok := s.l1.Get(key); ok && time.Now().Before(v.exp) {
			e := v.entry
			return &e, true
		}
	}
	if s.cfg.Exact.Enabled && s.rdb != nil {
		b, err := s.rdb.Get(ctx, key).Bytes()
		if err != nil {
			return nil, false
		}
		var e Entry
		if json.Unmarshal(b, &e) != nil {
			return nil, false
		}
		return &e, true
	}
	return nil, false
}

func (s *Store) Get(ctx context.Context, key string) (*Entry, HitKind, error) {
	if !s.cfg.Enabled {
		return nil, HitNone, nil
	}
	if s.cfg.L1.Enabled && s.l1 != nil {
		if v, ok := s.l1.Get(key); ok && time.Now().Before(v.exp) {
			s.Stats.L1Hits.Add(1)
			s.noteSaved(v.entry)
			return &v.entry, HitL1, nil
		}
	}
	if s.cfg.Exact.Enabled && s.rdb != nil {
		b, err := s.rdb.Get(ctx, key).Bytes()
		if err == redis.Nil {
			s.Stats.Misses.Add(1)
			return nil, HitNone, nil
		}
		if err != nil {
			s.Stats.Misses.Add(1)
			return nil, HitNone, err
		}
		var e Entry
		if err := json.Unmarshal(b, &e); err != nil {
			s.Stats.Misses.Add(1)
			return nil, HitNone, err
		}
		s.setL1(key, e)
		s.Stats.L2Hits.Add(1)
		s.noteSaved(e)
		return &e, HitL2, nil
	}
	s.Stats.Misses.Add(1)
	return nil, HitNone, nil
}

func (s *Store) Set(ctx context.Context, key string, e Entry) error {
	if !s.cfg.Enabled || !s.cfg.Exact.Enabled {
		return nil
	}
	e.CreatedAt = time.Now().UTC()
	s.setL1(key, e)
	if s.rdb == nil {
		return nil
	}
	b, err := json.Marshal(e)
	if err != nil {
		return err
	}
	e.Bytes = len(b)
	b, _ = json.Marshal(e)
	return s.rdb.Set(ctx, key, b, s.cfg.Exact.DefaultTTL).Err()
}

func (s *Store) Delete(ctx context.Context, key string) error {
	if s.l1 != nil {
		s.l1.Remove(key)
	}
	if s.rdb != nil && key != "" {
		return s.rdb.Del(ctx, key).Err()
	}
	return nil
}

func (s *Store) Flush(ctx context.Context, schema, ns string) error {
	if s.l1 != nil {
		s.l1.Purge()
	}
	if s.rdb == nil {
		return nil
	}
	pattern := "llmgw:" + schema + ":"
	if ns != "" {
		pattern += ns + ":"
	}
	pattern += "*"
	var cursor uint64
	for {
		keys, next, err := s.rdb.Scan(ctx, cursor, pattern, 200).Result()
		if err != nil {
			return err
		}
		if len(keys) > 0 {
			if err := s.rdb.Del(ctx, keys...).Err(); err != nil {
				return err
			}
		}
		cursor = next
		if cursor == 0 {
			return nil
		}
	}
}

func (s *Store) setL1(key string, e Entry) {
	if s.l1 == nil || !s.cfg.L1.Enabled {
		return
	}
	ttl := s.cfg.L1.TTL
	if ttl <= 0 {
		ttl = time.Minute
	}
	s.l1.Add(key, l1ent{entry: e, exp: time.Now().Add(ttl)})
}

func (s *Store) noteSaved(e Entry) {
	s.Stats.BytesSaved.Add(int64(e.Bytes))
	s.Stats.EstimatedInputTokensSaved.Add(int64(e.Completion.Usage.InputTokens))
	s.Stats.EstimatedOutputTokensSaved.Add(int64(e.Completion.Usage.OutputTokens))
}

func (s *Store) AddCostSaved(usd float64) {
	if usd <= 0 {
		return
	}
	s.Stats.CostSaved.Add(uint64(usd * 1e6))
}

func (s *Store) SemanticAllowed(workload string, hasTools bool) bool {
	return s.sem.AllowedFor(workload, hasTools)
}

func Age(e *Entry) string {
	if e == nil {
		return ""
	}
	return time.Since(e.CreatedAt).Truncate(time.Millisecond).String()
}
