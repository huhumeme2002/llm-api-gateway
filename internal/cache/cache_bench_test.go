package cache

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"llmgw/internal/config"
	"llmgw/internal/protocol"
)

func BenchmarkL1Get(b *testing.B) {
	mr := miniredis.RunT(b)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	st, err := New(config.CacheConfig{
		Enabled: true,
		L1:      config.L1Config{Enabled: true, MaxEntries: 128, TTL: time.Minute},
		Exact:   config.ExactConfig{Enabled: true, DefaultTTL: time.Hour},
	}, rdb)
	if err != nil {
		b.Fatal(err)
	}
	ctx := context.Background()
	_ = st.Set(ctx, "hot", Entry{Completion: protocol.Completion{Content: "x", Usage: protocol.Usage{InputTokens: 1, OutputTokens: 1}}})
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, _ = st.Get(ctx, "hot")
	}
}
