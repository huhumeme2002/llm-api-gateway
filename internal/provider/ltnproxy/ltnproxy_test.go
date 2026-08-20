package ltnproxy

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"llmgw/internal/config"
	"llmgw/internal/protocol"
)

func TestListModelsAndChat(t *testing.T) {
	var sawAuth, sawChat bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
			http.Error(w, "no auth", 401)
			return
		}
		sawAuth = true
		switch {
		case strings.HasSuffix(r.URL.Path, "/models"):
			_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"deepseek/deepseek-v4-flash","object":"model"}]}`))
		case strings.HasSuffix(r.URL.Path, "/chat/completions"):
			sawChat = true
			b, _ := io.ReadAll(r.Body)
			var parsed map[string]any
			_ = json.Unmarshal(b, &parsed)
			if parsed["model"] != "deepseek/deepseek-v4-flash" {
				t.Errorf("upstream model %v", parsed["model"])
			}
			_, _ = w.Write([]byte(`{"id":"c1","object":"chat.completion","choices":[{"message":{"role":"assistant","content":"pong"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":1,"prompt_tokens_details":{"cached_tokens":2}}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	a := New(config.ProviderConfig{Enabled: true, BaseURL: srv.URL, Timeout: 0}, "sk-test-ltn-not-real")
	if a.Name() != "ltnproxy" {
		t.Fatal(a.Name())
	}
	ms, err := a.ListModels(context.Background())
	if err != nil || len(ms) != 1 || ms[0].UpstreamID != "deepseek/deepseek-v4-flash" {
		t.Fatalf("%v %+v", err, ms)
	}
	if a.NativeProtocol(ms[0].ID) != protocol.ChatCompletions {
		t.Fatal("native")
	}
	body := []byte(`{"model":"deepseek/deepseek-v4-flash","messages":[{"role":"user","content":"hi"}]}`)
	res, err := a.Do(context.Background(), protocol.ChatCompletions, "deepseek/deepseek-v4-flash", body, false, nil, "default")
	if err != nil || res.Completion.Content != "pong" {
		t.Fatalf("%v %+v", err, res)
	}
	if res.Completion.Usage.CachedInputTokens != 2 {
		t.Fatalf("cached %d", res.Completion.Usage.CachedInputTokens)
	}
	if !sawAuth || !sawChat {
		t.Fatal("did not hit mock")
	}
	creds := a.ListCredentials()
	if len(creds) != 1 || creds[0].ID != "default" {
		t.Fatalf("%+v", creds)
	}
}
