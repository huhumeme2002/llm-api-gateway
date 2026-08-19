package protocol

import (
	"encoding/json"
	"time"
)

type Protocol string

const (
	ChatCompletions Protocol = "chat_completions"
	Responses       Protocol = "responses"
	Messages        Protocol = "messages"
)

type ToolCall struct {
	ID        string `json:"id,omitempty"`
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

type Usage struct {
	InputTokens        int `json:"input_tokens"`
	OutputTokens       int `json:"output_tokens"`
	CachedInputTokens  int `json:"cached_input_tokens"`
	CacheWriteTokens   int `json:"cache_write_tokens"`
	ReasoningTokens    int `json:"reasoning_tokens"`
	TotalTokens        int `json:"total_tokens"`
}

type Completion struct {
	ID           string     `json:"id"`
	Model        string     `json:"model"`
	Created      int64      `json:"created"`
	Content      string     `json:"content"`
	Reasoning    string     `json:"reasoning,omitempty"`
	ToolCalls    []ToolCall `json:"tool_calls,omitempty"`
	FinishReason string     `json:"finish_reason,omitempty"`
	Usage        Usage      `json:"usage"`
	StopReason   string     `json:"stop_reason,omitempty"`
}

type ParsedRequest struct {
	Protocol         Protocol
	Raw              json.RawMessage
	Model            string
	Stream           bool
	Temperature      *float64
	TopP             *float64
	MaxTokens        *int
	Seed             *int64
	Messages         json.RawMessage
	System           json.RawMessage
	Developer        json.RawMessage
	Tools            json.RawMessage
	ToolChoice       json.RawMessage
	ResponseFormat   json.RawMessage
	Reasoning        json.RawMessage
	ProviderParams   json.RawMessage
	PreviousResponse string
	HasTools         bool
	Workload         string
}

type StreamEvent struct {
	Event          string
	DeltaContent   string
	DeltaReasoning string
	ToolCalls      []ToolCall
	FinishReason   string
	Usage          *Usage
	Done           bool
	Err            error
	Raw            []byte // original data payload (no SSE framing)
}

func NowUnix() int64 { return time.Now().Unix() }

func MergeUsage(dst *Usage, src Usage) {
	if src.InputTokens > 0 {
		dst.InputTokens = src.InputTokens
	}
	if src.OutputTokens > 0 {
		dst.OutputTokens = src.OutputTokens
	}
	if src.CachedInputTokens > 0 {
		dst.CachedInputTokens = src.CachedInputTokens
	}
	if src.CacheWriteTokens > 0 {
		dst.CacheWriteTokens = src.CacheWriteTokens
	}
	if src.ReasoningTokens > 0 {
		dst.ReasoningTokens = src.ReasoningTokens
	}
	dst.TotalTokens = dst.InputTokens + dst.OutputTokens
}
