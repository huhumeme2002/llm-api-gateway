package ltnproxy

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"llmgw/internal/config"
	"llmgw/internal/protocol"
	"llmgw/internal/provider"
)

const Name = "ltnproxy"

// OpenAI-compatible chat API observed 2026-08-20 against https://ltnproxy.com/v1
// Auth: Authorization: Bearer $LTN_API_KEY
// Catalog: GET /models
// Chat: POST /chat/completions (streaming supported)
// Messages / Responses: not advertised — gateway translates to Chat Completions.

type Adapter struct {
	pool *provider.Pool
}

func New(cfg config.ProviderConfig, apiKey string) *Adapter {
	if cfg.Name == "" {
		cfg.Name = Name
	}
	if strings.TrimRight(cfg.BaseURL, "/") == "" {
		cfg.BaseURL = "https://ltnproxy.com/v1"
	}
	return &Adapter{pool: provider.NewPool(cfg, apiKey, nil)}
}

func (a *Adapter) Name() string { return Name }

func (a *Adapter) NativeProtocol(string) protocol.Protocol { return protocol.ChatCompletions }

func (a *Adapter) SupportsProtocol(_ string, p protocol.Protocol) bool {
	return p == protocol.ChatCompletions
}

func (a *Adapter) ListCredentials() []provider.CredentialInfo { return a.pool.Infos() }

func (a *Adapter) ActiveRequests(credID string) int64 {
	if s := a.pool.Get(credID); s != nil {
		return s.Active.Load()
	}
	return 0
}

func (a *Adapter) ListModels(ctx context.Context) ([]provider.Model, error) {
	slot := a.pool.First()
	if slot == nil {
		return nil, errNoCred
	}
	b, _, _, err := slot.Client.GetJSON(ctx, "models")
	if err != nil {
		return nil, err
	}
	var wrap struct {
		Data []struct {
			ID            string `json:"id"`
			Name          string `json:"name"`
			OwnedBy       string `json:"owned_by"`
			ContextLength int    `json:"context_length"`
		} `json:"data"`
	}
	if err := json.Unmarshal(b, &wrap); err != nil {
		return nil, err
	}
	out := make([]provider.Model, 0, len(wrap.Data))
	for _, m := range wrap.Data {
		name := m.Name
		if name == "" {
			name = m.ID
		}
		out = append(out, provider.Model{
			ID:                      m.ID,
			Provider:                Name,
			UpstreamID:              m.ID,
			DisplayName:             name,
			NativeProtocol:          protocol.ChatCompletions,
			SupportsStreaming:       true,
			SupportsTools:           true,
			SupportsReasoning:       true,
			SupportsResponses:       false,
			SupportsMessages:        false,
			SupportsChatCompletions: true,
			ContextWindow:           m.ContextLength,
			Health:                  "unknown",
		})
	}
	return out, nil
}

func (a *Adapter) Do(ctx context.Context, native protocol.Protocol, modelID string, raw []byte, stream bool, on provider.StreamHandler, credID string) (provider.Result, error) {
	slot := a.pool.Get(credID)
	if slot == nil {
		return provider.Result{Status: 503}, errNoCred
	}
	slot.Begin()
	defer slot.End()
	start := time.Now()
	body, status, hdr, rc, err := slot.Client.PostJSON(ctx, "chat/completions", raw, stream)
	res := provider.Result{Status: status, Latency: time.Since(start), Headers: hdr, CredentialID: slot.Info.ID}
	if err != nil {
		return res, err
	}
	if stream {
		comp, err := provider.StreamBody(ctx, rc, protocol.ChatCompletions, on)
		res.Latency = time.Since(start)
		res.Completion = comp
		if res.Completion.Model == "" {
			res.Completion.Model = modelID
		}
		return res, err
	}
	comp, err := protocol.ParseUpstreamCompletion(protocol.ChatCompletions, body)
	res.Completion = comp
	if res.Completion.Model == "" {
		res.Completion.Model = modelID
	}
	_ = native
	return res, err
}

func (a *Adapter) Health(ctx context.Context) provider.Health {
	return a.pool.HealthAny(ctx, Name)
}

func (a *Adapter) HealthCredential(ctx context.Context, credID string) provider.Health {
	return a.pool.HealthOne(ctx, Name, credID)
}

var errNoCred = errString("no credential")

type errString string

func (e errString) Error() string { return string(e) }
