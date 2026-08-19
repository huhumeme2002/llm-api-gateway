package singleflight

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"llmgw/internal/config"
)

func TestLocalCoalesce(t *testing.T) {
	g := New(config.SingleflightCfg{Enabled: true, LockTTL: time.Minute, WaitTimeout: time.Minute}, nil)
	var n atomic.Int32
	var wg sync.WaitGroup
	const N = 100
	wg.Add(N)
	for i := 0; i < N; i++ {
		go func() {
			defer wg.Done()
			_, err, _ := g.Do(context.Background(), "k", func() (any, error) {
				n.Add(1)
				time.Sleep(30 * time.Millisecond)
				return "ok", nil
			})
			if err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if n.Load() != 1 {
		t.Fatalf("upstream calls %d want 1", n.Load())
	}
	if g.Saved.Load() < 90 {
		t.Fatalf("saved %d", g.Saved.Load())
	}
}

func TestRedisWaiterReleased(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	g := New(config.SingleflightCfg{Enabled: true, LockTTL: time.Minute, WaitTimeout: time.Minute}, rdb)
	ctx := context.Background()
	started := make(chan struct{})
	done := make(chan struct{})
	go func() {
		_, _, _ = g.Do(ctx, "rk", func() (any, error) {
			close(started)
			time.Sleep(80 * time.Millisecond)
			return 1, nil
		})
		close(done)
	}()
	<-started
	_, err, shared := g.Do(ctx, "rk", func() (any, error) {
		t.Fatal("waiter must not run fn after lock")
		return nil, nil
	})
	if !shared {
		t.Fatal("expected shared")
	}
	if err != nil && !IsWaiterReleased(err) {
		t.Fatal(err)
	}
	<-done
}
