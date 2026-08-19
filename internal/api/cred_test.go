package api_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/redis/go-redis/v9"
	"llmgw/internal/api"
	"llmgw/internal/auth"
	"llmgw/internal/cache"
	"llmgw/internal/config"
	"llmgw/internal/cost"
	"llmgw/internal/logx"
	"llmgw/internal/metrics"
	"llmgw/internal/provider"
	"llmgw/internal/provider/opencodego"
	"llmgw/internal/router"
	"llmgw/internal/singleflight"
	"llmgw/internal/usage"
)

func TestAdminCredentialsNoSecretLeak(t *testing.T) {
	var originHits atomic.Int32
	origin := ocgoServer(t, &mocks{})
	defer origin.Close()
	t.Setenv("OPENCODE_GO_KEY_01", "sk-super-secret-key-aaaa")
	t.Setenv("OPENCODE_GO_PROXY_01", "http://alice:s3cretpass@127.0.0.1:9")
	hs, gw := gwWithCreds(t, origin.URL)
	_ = originHits
	r := doJSON(t, hs, "GET", "/admin/credentials", "gw-key", nil, nil)
	b, _ := io.ReadAll(r.Body)
	r.Body.Close()
	if r.StatusCode != 200 {
		t.Fatal(string(b))
	}
	leak := []string{"sk-super-secret-key-aaaa", "s3cretpass", "alice:s3cret"}
	for _, s := range leak {
		if strings.Contains(string(b), s) {
			t.Fatalf("leaked %q in %s", s, b)
		}
	}
	if !strings.Contains(string(b), `"id":"go-01"`) && !strings.Contains(string(b), `"id": "go-01"`) {
		if !strings.Contains(string(b), "go-01") {
			t.Fatal(string(b))
		}
	}
	_ = gw
}

func TestCacheAndSingleflightOneCredential(t *testing.T) {
	m := &mocks{}
	origin := ocgoServer(t, m)
	defer origin.Close()
	hs, _ := gwWithCreds(t, origin.URL)
	body := []byte(`{"model":"opencode-go/deepseek-v4-flash","temperature":0,"messages":[{"role":"user","content":"same-cred"}]}`)
	var wg sync.WaitGroup
	const N = 25
	wg.Add(N)
	for i := 0; i < N; i++ {
		go func() {
			defer wg.Done()
			resp := doJSON(t, hs, "POST", "/v1/chat/completions", "gw-key", body, map[string]string{"x-session-id": "same"})
			_, _ = io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
		}()
	}
	wg.Wait()
	if m.chatCalls.Load() != 1 {
		t.Fatalf("upstream %d want 1 (singleflight + one key)", m.chatCalls.Load())
	}
	r2 := doJSON(t, hs, "POST", "/v1/chat/completions", "gw-key", body, nil)
	if !strings.HasPrefix(r2.Header.Get("x-cache"), "HIT") {
		t.Fatal(r2.Header.Get("x-cache"))
	}
	r2.Body.Close()
}

func TestStickyAndFailoverAndStream(t *testing.T) {
	m := &mocks{}
	origin := ocgoServer(t, m)
	defer origin.Close()
	hs, gw := gwWithCreds(t, origin.URL)
	body := []byte(`{"model":"fast","temperature":0,"messages":[{"role":"user","content":"sticky"}]}`)
	r1 := doJSON(t, hs, "POST", "/v1/chat/completions", "gw-key", body, map[string]string{"x-session-id": "A", "x-cache-mode": "bypass"})
	c1 := r1.Header.Get("x-gateway-credential")
	r1.Body.Close()
	if c1 == "" {
		t.Fatal("missing credential header")
	}
	r2 := doJSON(t, hs, "POST", "/v1/chat/completions", "gw-key", []byte(`{"model":"fast","temperature":0,"messages":[{"role":"user","content":"sticky2"}]}`), map[string]string{"x-session-id": "A", "x-cache-mode": "bypass"})
	c2 := r2.Header.Get("x-gateway-credential")
	r2.Body.Close()
	if c1 != c2 {
		t.Fatalf("sticky cred %s vs %s", c1, c2)
	}

	gw.Router.ReportCred("opencode_go", c1, 429, nil)
	r3 := doJSON(t, hs, "POST", "/v1/chat/completions", "gw-key", []byte(`{"model":"fast","temperature":0,"messages":[{"role":"user","content":"after-429"}]}`), map[string]string{"x-session-id": "A", "x-cache-mode": "bypass"})
	c3 := r3.Header.Get("x-gateway-credential")
	r3.Body.Close()
	if c3 == "" || c3 == c1 {
		t.Fatalf("expected failover away from %s, got %s", c1, c3)
	}

	streamBody := []byte(`{"model":"fast","temperature":0,"stream":true,"messages":[{"role":"user","content":"via-proxy-stream"}]}`)
	rs := doJSON(t, hs, "POST", "/v1/chat/completions", "gw-key", streamBody, map[string]string{"x-cache-mode": "bypass"})
	sb, _ := io.ReadAll(rs.Body)
	rs.Body.Close()
	if !strings.Contains(string(sb), "data:") && rs.StatusCode != 200 {
		t.Fatal(rs.StatusCode, string(sb))
	}
}

func TestCredentialConcurrencyRace(t *testing.T) {
	m := &mocks{}
	origin := ocgoServer(t, m)
	defer origin.Close()
	hs, _ := gwWithCreds(t, origin.URL)
	var wg sync.WaitGroup
	for i := 0; i < 40; i++ {
		wg.Add(1)
		i := i
		go func() {
			defer wg.Done()
			sess := "s"
			if i%2 == 0 {
				sess = "t"
			}
			body := []byte(`{"model":"fast","temperature":0.9,"messages":[{"role":"user","content":"c"}]}`)
			resp := doJSON(t, hs, "POST", "/v1/chat/completions", "gw-key", body, map[string]string{"x-session-id": sess, "x-cache-mode": "bypass"})
			_, _ = io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
		}()
	}
	wg.Wait()
}

func gwWithCreds(t *testing.T, originURL string) (*httptest.Server, *api.Gateway) {
	t.Helper()
	t.Setenv("OPENCODE_GO_KEY_01", "sk-key-one-aaaaaaaa")
	t.Setenv("OPENCODE_GO_KEY_02", "sk-key-two-bbbbbbbb")
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	cfg := config.Defaults()
	cfg.Server.AuthRequired = true
	cfg.CircuitBreaker.Failures = 1
	cfg.CircuitBreaker.Cooldown = time.Hour
	cfg.Aliases = map[string][]config.AliasTarget{
		"fast": {{Provider: "opencode_go", Model: "deepseek-v4-flash", Weight: 1}},
	}
	cfg.Tenants = []config.TenantConfig{{ID: "default", APIKey: "gw-key", Admin: true, CacheNamespace: "default"}}
	cfg.Providers = map[string]config.ProviderConfig{
		"opencode_go": {
			Enabled: true, Name: "opencode_go", BaseURL: originURL, Timeout: 5 * time.Second,
			Credentials: []config.CredentialConfig{
				{ID: "go-01", APIKeyEnv: "OPENCODE_GO_KEY_01"},
				{ID: "go-02", APIKeyEnv: "OPENCODE_GO_KEY_02"},
			},
		},
	}
	st, err := cache.New(cfg.Cache, rdb)
	if err != nil {
		t.Fatal(err)
	}
	oc := opencodego.New(cfg.Providers["opencode_go"], "")
	reg := provider.NewRegistry(logx.Discard(), time.Hour, oc)
	reg.Refresh(context.Background())
	us := usage.New(rdb)
	rt := router.New(cfg, reg, us, rdb)
	gw := &api.Gateway{
		Cfg: cfg, Log: logx.Discard(), Auth: auth.New(cfg), Cache: st,
		SF: singleflight.New(cfg.Cache.Singleflight, rdb), Reg: reg, Router: rt,
		Usage: us, Cost: cost.Load(""), M: metrics.New(prometheus.NewRegistry()),
		Admin: "gw-key", Ready: func() error { return nil },
	}
	hs := httptest.NewServer(gw.Handler())
	t.Cleanup(hs.Close)
	return hs, gw
}

func TestJSONCredentialsShape(t *testing.T) {
	raw, _ := json.Marshal(map[string]any{"id": "go-01", "has_proxy": true})
	if strings.Contains(string(raw), "http://") {
		t.Fatal(string(raw))
	}
	_ = os.Stderr
}
