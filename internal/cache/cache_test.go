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

func testStore(t *testing.T) (*Store, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	st, err := New(config.CacheConfig{
		Enabled: true,
		L1:      config.L1Config{Enabled: true, MaxEntries: 16, TTL: time.Minute},
		Exact:   config.ExactConfig{Enabled: true, DefaultTTL: time.Hour},
	}, rdb)
	if err != nil {
		t.Fatal(err)
	}
	return st, mr
}

func TestL1AndL2Hits(t *testing.T) {
	st, _ := testStore(t)
	ctx := context.Background()
	e := Entry{Completion: protocol.Completion{Content: "ok", Usage: protocol.Usage{InputTokens: 5, OutputTokens: 2}}, Provider: "p", Model: "m"}
	if err := st.Set(ctx, "k1", e); err != nil {
		t.Fatal(err)
	}
	got, kind, err := st.Get(ctx, "k1")
	if err != nil || got == nil || kind != HitL1 {
		t.Fatalf("l1 %v %v %v", kind, got, err)
	}
	st.l1.Purge()
	got, kind, err = st.Get(ctx, "k1")
	if err != nil || got == nil || kind != HitL2 {
		t.Fatalf("l2 %v %v %v", kind, got, err)
	}
	_, kind, _ = st.Get(ctx, "missing")
	if kind != HitNone {
		t.Fatal(kind)
	}
}

func TestEligiblePolicy(t *testing.T) {
	zero := 0.0
	one := 1.0
	if !Eligible(ModeForce, &protocol.ParsedRequest{Temperature: &one}) {
		t.Fatal("force")
	}
	if Eligible(ModeBypass, &protocol.ParsedRequest{Temperature: &zero}) {
		t.Fatal("bypass")
	}
	if !Eligible(ModeAuto, &protocol.ParsedRequest{Temperature: &zero}) {
		t.Fatal("temp0")
	}
	if Eligible(ModeAuto, &protocol.ParsedRequest{Temperature: &one}) {
		t.Fatal("temp1")
	}
	if Eligible(ModeAuto, &protocol.ParsedRequest{Temperature: &zero, PreviousResponse: "r1"}) {
		t.Fatal("stateful")
	}
	seed := int64(7)
	if !Eligible(ModeAuto, &protocol.ParsedRequest{Seed: &seed, Temperature: &one}) {
		t.Fatal("seed")
	}
}

func TestSemanticDisabledByDefault(t *testing.T) {
	s := Semantic{Enabled: false}
	if s.AllowedFor("docs", false) {
		t.Fatal("disabled")
	}
	s.Enabled = true
	s.Allowed = []string{"docs"}
	if s.AllowedFor("docs", true) {
		t.Fatal("tools")
	}
	if !s.AllowedFor("docs", false) {
		t.Fatal("allow")
	}
}
