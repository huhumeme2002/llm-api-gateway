package router

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"llmgw/internal/config"
	"llmgw/internal/provider"
	"llmgw/internal/usage"
)

func TestStickyCredentialRedis(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	cfg := config.Defaults()
	cfg.Routing.StickySessions = true
	cfg.Routing.StickyTTL = time.Hour
	rt := New(cfg, provider.NewRegistry(nil, 0), usage.New(nil), rdb)
	ctx := context.Background()
	rt.RememberStickyCred(ctx, "t", "sessA", "opencode_go", "deepseek-v4-flash", "go-01")
	p, m, c, ok := rt.StickyCred(ctx, "t", "sessA")
	if !ok || p != "opencode_go" || m != "deepseek-v4-flash" || c != "go-01" {
		t.Fatalf("%s %s %s %v", p, m, c, ok)
	}
	ids := rt.OrderCredentials(ctx, "t", "sessA", "opencode_go", []string{"go-02", "go-01"})
	if ids[0] != "go-01" {
		t.Fatal(ids)
	}
}

func TestCredentialCircuitFailover(t *testing.T) {
	cfg := config.Defaults()
	cfg.CircuitBreaker.Failures = 1
	cfg.CircuitBreaker.Cooldown = time.Hour
	rt := New(cfg, provider.NewRegistry(nil, 0), usage.New(nil), nil)
	rt.ReportCred("opencode_go", "go-01", 429, nil)
	if rt.AllowCred("opencode_go", "go-01") {
		t.Fatal("open circuit should block")
	}
	if !rt.AllowCred("opencode_go", "go-02") {
		t.Fatal("other cred must stay closed/allow")
	}
	ids := rt.OrderCredentials(context.Background(), "t", "", "opencode_go", []string{"go-01", "go-02"})
	if len(ids) != 1 || ids[0] != "go-02" {
		t.Fatal(ids)
	}
	if rt.CircuitStateCred("opencode_go", "go-01") != Open {
		t.Fatal(rt.CircuitStateCred("opencode_go", "go-01"))
	}
}
