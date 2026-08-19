package singleflight

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"
	"golang.org/x/sync/singleflight"
	"llmgw/internal/config"
)

var ErrTimeout = errors.New("singleflight wait timeout")

type Group struct {
	cfg   config.SingleflightCfg
	rdb   *redis.Client
	local singleflight.Group
	Saved atomic.Int64
}

func New(cfg config.SingleflightCfg, rdb *redis.Client) *Group {
	return &Group{cfg: cfg, rdb: rdb}
}

func (g *Group) Do(ctx context.Context, key string, fn func() (any, error)) (any, error, bool) {
	if !g.cfg.Enabled {
		v, err := fn()
		return v, err, false
	}
	shared := false
	v, err, localShared := g.local.Do(key, func() (any, error) {
		if g.rdb == nil {
			return fn()
		}
		return g.doRedis(ctx, key, fn, &shared)
	})
	if shared || localShared {
		g.Saved.Add(1)
	}
	return v, err, shared || localShared
}

func (g *Group) doRedis(ctx context.Context, key string, fn func() (any, error), shared *bool) (any, error) {
	lockKey := "llmgw:sf:" + key
	token := nonce()
	ttl := g.cfg.LockTTL
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	ok, err := g.rdb.SetNX(ctx, lockKey, token, ttl).Result()
	if err != nil {
		return fn()
	}
	if ok {
		defer g.unlock(ctx, lockKey, token)
		return fn()
	}
	*shared = true
	wait := g.cfg.WaitTimeout
	if wait <= 0 {
		wait = 4 * time.Minute
	}
	deadline := time.Now().Add(wait)
	for time.Now().Before(deadline) {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		// lock gone => winner finished (or expired); caller re-checks cache
		n, err := g.rdb.Exists(ctx, lockKey).Result()
		if err != nil {
			return nil, err
		}
		if n == 0 {
			return nil, errWaiterReleased
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(25 * time.Millisecond):
		}
	}
	return nil, ErrTimeout
}

var errWaiterReleased = errors.New("singleflight waiter released")

func IsWaiterReleased(err error) bool {
	return errors.Is(err, errWaiterReleased)
}

func (g *Group) unlock(ctx context.Context, key, token string) {
	// best-effort compare-and-delete
	script := redis.NewScript(`if redis.call("get", KEYS[1]) == ARGV[1] then return redis.call("del", KEYS[1]) else return 0 end`)
	_, _ = script.Run(ctx, g.rdb, []string{key}, token).Result()
}

func nonce() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// Barrier is a test helper: N callers wait until all have arrived.
func Barrier(n int) func() {
	var mu sync.Mutex
	var wg sync.WaitGroup
	wg.Add(n)
	return func() {
		mu.Lock()
		wg.Done()
		mu.Unlock()
		wg.Wait()
	}
}
