package provider

import (
	"context"
	"io"
	"net/http"
	"strings"
	"time"

	"llmgw/internal/protocol"
)

type Protocol = protocol.Protocol

type Model struct {
	ID                      string `json:"id"`
	Provider                string `json:"provider"`
	UpstreamID              string `json:"upstream_model_id"`
	DisplayName             string `json:"display_name,omitempty"`
	NativeProtocol          Protocol `json:"protocol"`
	SupportsStreaming       bool   `json:"supports_streaming"`
	SupportsTools           bool   `json:"supports_tools"`
	SupportsReasoning       bool   `json:"supports_reasoning"`
	SupportsResponses       bool   `json:"supports_responses"`
	SupportsMessages        bool   `json:"supports_messages"`
	SupportsChatCompletions bool   `json:"supports_chat_completions"`
	ContextWindow           int    `json:"context_window"`
	Health                  string `json:"health"`
}

type Health struct {
	Name      string    `json:"name"`
	Healthy   bool      `json:"healthy"`
	Message   string    `json:"message,omitempty"`
	CheckedAt time.Time `json:"checked_at"`
}

type Result struct {
	Completion   protocol.Completion
	Status       int
	Latency      time.Duration
	Headers      http.Header
	CredentialID string
}

type StreamHandler func(protocol.StreamEvent) error

type Provider interface {
	Name() string
	ListModels(ctx context.Context) ([]Model, error)
	ListCredentials() []CredentialInfo
	NativeProtocol(modelID string) Protocol
	SupportsProtocol(modelID string, p Protocol) bool
	Do(ctx context.Context, native Protocol, modelID string, raw []byte, stream bool, onStream StreamHandler, credID string) (Result, error)
	Health(ctx context.Context) Health
	HealthCredential(ctx context.Context, credID string) Health
	ActiveRequests(credID string) int64
}

func NormalizeName(s string) string {
	s = strings.TrimSpace(strings.ToLower(s))
	s = strings.ReplaceAll(s, "-", "_")
	return s
}

func PublicProviderID(name string) string {
	switch NormalizeName(name) {
	case "opencode_go", "opencodego", "opencode-go":
		return "opencode-go"
	case "commandcode", "command_code", "command-code":
		return "commandcode"
	default:
		return name
	}
}

func Drain(r io.ReadCloser) {
	if r == nil {
		return
	}
	_, _ = io.Copy(io.Discard, r)
	_ = r.Close()
}
