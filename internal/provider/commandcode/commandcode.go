package commandcode

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"llmgw/internal/config"
	"llmgw/internal/protocol"
	"llmgw/internal/provider"
)

const Name = "commandcode"

// Official docs 2026-08-19: https://commandcode.ai/docs/provider
// Base: https://api.commandcode.ai/provider/v1
// Auth: Authorization: Bearer or x-api-key
// Chat Completions + Messages. No Responses API.
// Claude* models MUST use /messages; others MUST use /chat/completions.

type Adapter struct {
	pool *provider.Pool
}

func New(cfg config.ProviderConfig, apiKey string) *Adapter {
	if cfg.Name == "" {
		cfg.Name = Name
	}
	extra := map[string]string{}
	if cfg.ZDR {
		extra["x-cmdc-zdr"] = "1"
	}
	return &Adapter{pool: provider.NewPool(cfg, apiKey, extra)}
}

func (a *Adapter) Name() string { return Name }

func (a *Adapter) NativeProtocol(modelID string) protocol.Protocol {
	id := strings.ToLower(modelID)
	if strings.HasPrefix(id, "claude") {
		return protocol.Messages
	}
	return protocol.ChatCompletions
}

func (a *Adapter) SupportsProtocol(modelID string, p protocol.Protocol) bool {
	return a.NativeProtocol(modelID) == p
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
			ContextLength int    `json:"context_length"`
		} `json:"data"`
	}
	if err := json.Unmarshal(b, &wrap); err != nil {
		return nil, err
	}
	out := make([]provider.Model, 0, len(wrap.Data))
	for _, m := range wrap.Data {
		nat := a.NativeProtocol(m.ID)
		out = append(out, provider.Model{
			ID:                      m.ID,
			Provider:                Name,
			UpstreamID:              m.ID,
			DisplayName:             m.Name,
			NativeProtocol:          nat,
			SupportsStreaming:       true,
			SupportsTools:           true,
			SupportsReasoning:       true,
			SupportsResponses:       false,
			SupportsMessages:        nat == protocol.Messages,
			SupportsChatCompletions: nat == protocol.ChatCompletions,
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
	path := "chat/completions"
	if native == protocol.Messages {
		path = "messages"
	}
	start := time.Now()
	body, status, hdr, rc, err := slot.Client.PostJSON(ctx, path, raw, stream)
	res := provider.Result{Status: status, Latency: time.Since(start), Headers: hdr, CredentialID: slot.Info.ID}
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
	return a.pool.HealthAny(ctx, Name)
}

func (a *Adapter) HealthCredential(ctx context.Context, credID string) provider.Health {
	return a.pool.HealthOne(ctx, Name, credID)
}

var errNoCred = errString("no credential")

type errString string

func (e errString) Error() string { return string(e) }
