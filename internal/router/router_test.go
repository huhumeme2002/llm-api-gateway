package router

import (
	"context"
	"testing"
	"time"

	"llmgw/internal/config"
	"llmgw/internal/provider"
	"llmgw/internal/usage"
)

type stub struct{ name string }

func (s stub) Name() string { return s.name }
func (s stub) ListModels(context.Context) ([]provider.Model, error) {
	return []provider.Model{{ID: "m", UpstreamID: "m", Provider: s.name}}, nil
}
func (s stub) NativeProtocol(string) provider.Protocol { return "chat_completions" }
func (s stub) SupportsProtocol(string, provider.Protocol) bool {
	return true
}
func (s stub) Do(context.Context, provider.Protocol, string, []byte, bool, provider.StreamHandler, string) (provider.Result, error) {
	return provider.Result{}, nil
}
func (s stub) Health(context.Context) provider.Health { return provider.Health{Healthy: true} }
func (s stub) HealthCredential(context.Context, string) provider.Health {
	return provider.Health{Healthy: true}
}
func (s stub) ListCredentials() []provider.CredentialInfo {
	return []provider.CredentialInfo{{ID: "default", ProxyKind: "none"}}
}
func (s stub) ActiveRequests(string) int64 { return 0 }

func TestResolveExactAndAlias(t *testing.T) {
	cfg := config.Defaults()
	cfg.Aliases = map[string][]config.AliasTarget{
		"fast": {{Provider: "opencode_go", Model: "deepseek-v4-flash", Weight: 1}},
	}
	reg := provider.NewRegistry(nil, 0, stub{name: "opencode_go"}, stub{name: "commandcode"})
	reg.Refresh(context.Background())
	rt := New(cfg, reg, usage.New(nil), nil)
	cs, err := rt.Resolve("opencode-go/deepseek-v4-flash")
	if err != nil || len(cs) != 1 || cs[0].Model != "deepseek-v4-flash" {
		t.Fatalf("%v %+v", err, cs)
	}
	cs, err = rt.Resolve("fast")
	if err != nil || len(cs) != 1 {
		t.Fatal(err, cs)
	}
}

func TestCircuitBreaker(t *testing.T) {
	b := newBreaker(config.CircuitBreakerConfig{Failures: 2, Cooldown: 30 * time.Millisecond, HalfOpenMax: 1})
	if !b.Allow() {
		t.Fatal("closed")
	}
	b.Failure()
	b.Failure()
	if b.State() != Open {
		t.Fatal(b.State())
	}
	if b.Allow() {
		t.Fatal("open")
	}
	time.Sleep(40 * time.Millisecond)
	if !b.Allow() {
		t.Fatal("half")
	}
	b.Success()
	if b.State() != Closed {
		t.Fatal(b.State())
	}
}

func TestSticky(t *testing.T) {
	cfg := config.Defaults()
	cfg.Routing.StickySessions = true
	cfg.Routing.StickyTTL = time.Hour
	reg := provider.NewRegistry(nil, 0, stub{name: "opencode_go"}, stub{name: "commandcode"})
	rt := New(cfg, reg, usage.New(nil), nil)
	ctx := context.Background()
	rt.RememberSticky(ctx, "t", "s1", "opencode_go", "m")
	cs := []Candidate{
		{Provider: "commandcode", Model: "x", Weight: 10},
		{Provider: "opencode_go", Model: "m", Weight: 1},
	}
	got := rt.Pick(ctx, "t", "s1", cs)
	if got[0].Provider != "opencode_go" {
		t.Fatalf("%+v", got)
	}
}
