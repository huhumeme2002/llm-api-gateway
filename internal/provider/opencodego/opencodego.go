package opencodego

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"time"

	"llmgw/internal/config"
	"llmgw/internal/protocol"
	"llmgw/internal/provider"
)

const Name = "opencode_go"

// Official docs 2026-08-19: https://opencode.ai/docs/go/
// Base: https://opencode.ai/zen/go/v1
// Auth: Authorization: Bearer
// Chat: most models. Responses: grok-4.5, gpt-5.6-luna. Messages: MiniMax + Qwen.

type Adapter struct {
	http *provider.HTTPClient
	mu   sync.RWMutex
	prot map[string]protocol.Protocol
}

func New(cfg config.ProviderConfig, apiKey string) *Adapter {
	a := &Adapter{
		http: provider.NewHTTP(provider.HTTPConfig{
			Name:    Name,
			BaseURL: strings.TrimRight(cfg.BaseURL, "/"),
			APIKey:  apiKey,
			Timeout: cfg.Timeout,
		}),
		prot: map[string]protocol.Protocol{},
	}
	a.seedProtocols()
	return a
}

func (a *Adapter) Name() string { return Name }

func (a *Adapter) seedProtocols() {
	chat := []string{
		"glm-5.3", "glm-5.2", "glm-5.1", "glm-5",
		"kimi-k3", "kimi-k2.7-code", "kimi-k2.6", "kimi-k2.5",
		"deepseek-v4-pro", "deepseek-v4-flash",
		"mimo-v2.5", "mimo-v2.5-pro", "mimo-v2-pro", "mimo-v2-omni",
		"hy3", "hy3-preview",
	}
	for _, id := range chat {
		a.prot[id] = protocol.ChatCompletions
	}
	for _, id := range []string{"grok-4.5", "gpt-5.6-luna"} {
		a.prot[id] = protocol.Responses
	}
	for _, id := range []string{
		"minimax-m3", "minimax-m2.7", "minimax-m2.5",
		"qwen3.8-max", "qwen3.7-max", "qwen3.7-plus", "qwen3.6-plus", "qwen3.5-plus",
	} {
		a.prot[id] = protocol.Messages
	}
}

func (a *Adapter) NativeProtocol(modelID string) protocol.Protocol {
	a.mu.RLock()
	if p, ok := a.prot[modelID]; ok {
		a.mu.RUnlock()
		return p
	}
	a.mu.RUnlock()
	id := strings.ToLower(modelID)
	switch {
	case strings.HasPrefix(id, "minimax-") || strings.HasPrefix(id, "qwen"):
		return protocol.Messages
	case id == "grok-4.5" || strings.HasPrefix(id, "gpt-5.6-luna"):
		return protocol.Responses
	default:
		return protocol.ChatCompletions
	}
}

func (a *Adapter) SupportsProtocol(modelID string, p protocol.Protocol) bool {
	return a.NativeProtocol(modelID) == p
}

func (a *Adapter) ListModels(ctx context.Context) ([]provider.Model, error) {
	b, _, _, err := a.http.GetJSON(ctx, "models")
	if err != nil {
		return nil, err
	}
	var wrap struct {
		Data []struct {
			ID      string `json:"id"`
			OwnedBy string `json:"owned_by"`
		} `json:"data"`
	}
	if err := json.Unmarshal(b, &wrap); err != nil {
		return nil, err
	}
	out := make([]provider.Model, 0, len(wrap.Data))
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, m := range wrap.Data {
		nat := a.lookupLocked(m.ID)
		a.prot[m.ID] = nat
		out = append(out, provider.Model{
			ID:                      m.ID,
			Provider:                Name,
			UpstreamID:              m.ID,
			DisplayName:             m.ID,
			NativeProtocol:          nat,
			SupportsStreaming:       true,
			SupportsTools:           true,
			SupportsReasoning:       true,
			SupportsResponses:       nat == protocol.Responses,
			SupportsMessages:        nat == protocol.Messages,
			SupportsChatCompletions: nat == protocol.ChatCompletions,
			Health:                  "unknown",
		})
	}
	return out, nil
}

func (a *Adapter) lookupLocked(id string) protocol.Protocol {
	if p, ok := a.prot[id]; ok {
		return p
	}
	return a.NativeProtocol(id)
}

func (a *Adapter) Do(ctx context.Context, native protocol.Protocol, modelID string, raw []byte, stream bool, on provider.StreamHandler) (provider.Result, error) {
	path := pathFor(native)
	start := time.Now()
	body, status, hdr, rc, err := a.http.PostJSON(ctx, path, raw, stream)
	res := provider.Result{Status: status, Latency: time.Since(start), Headers: hdr}
	if err != nil {
		return res, err
	}
	if stream {
		comp, err := provider.StreamBody(ctx, rc, native, on)
		res.Latency = time.Since(start)
		res.Completion = comp
		if res.Completion.Model == "" {
			res.Completion.Model = modelID
		}
		return res, err
	}
	comp, err := protocol.ParseUpstreamCompletion(native, body)
	res.Completion = comp
	if res.Completion.Model == "" {
		res.Completion.Model = modelID
	}
	return res, err
}

func (a *Adapter) Health(ctx context.Context) provider.Health {
	h := provider.Health{Name: Name, CheckedAt: time.Now().UTC()}
	_, status, _, err := a.http.GetJSON(ctx, "models")
	if err != nil {
		h.Healthy = false
		h.Message = err.Error()
		return h
	}
	h.Healthy = status < 300
	return h
}

func pathFor(p protocol.Protocol) string {
	switch p {
	case protocol.Messages:
		return "messages"
	case protocol.Responses:
		return "responses"
	default:
		return "chat/completions"
	}
}
