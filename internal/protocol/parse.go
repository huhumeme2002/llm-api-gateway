package protocol

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/tidwall/gjson"
	"llmgw/internal/canonical"
)

func Parse(proto Protocol, body []byte) (*ParsedRequest, error) {
	if !json.Valid(body) {
		return nil, fmt.Errorf("invalid json")
	}
	p := &ParsedRequest{Protocol: proto, Raw: append(json.RawMessage(nil), body...)}
	root := gjson.ParseBytes(body)
	p.Model = root.Get("model").String()
	p.Stream = root.Get("stream").Bool()
	if root.Get("temperature").Exists() {
		v := root.Get("temperature").Float()
		p.Temperature = &v
	}
	if root.Get("top_p").Exists() {
		v := root.Get("top_p").Float()
		p.TopP = &v
	}
	if root.Get("seed").Exists() {
		v := root.Get("seed").Int()
		p.Seed = &v
	}
	if root.Get("max_tokens").Exists() {
		v := int(root.Get("max_tokens").Int())
		p.MaxTokens = &v
	} else if root.Get("max_output_tokens").Exists() {
		v := int(root.Get("max_output_tokens").Int())
		p.MaxTokens = &v
	} else if root.Get("max_completion_tokens").Exists() {
		v := int(root.Get("max_completion_tokens").Int())
		p.MaxTokens = &v
	}
	p.PreviousResponse = root.Get("previous_response_id").String()
	if root.Get("tools").Exists() {
		p.Tools = json.RawMessage(root.Get("tools").Raw)
		p.HasTools = len(bytes.TrimSpace(p.Tools)) > 2
	}
	if root.Get("tool_choice").Exists() {
		p.ToolChoice = json.RawMessage(root.Get("tool_choice").Raw)
	}
	if root.Get("response_format").Exists() {
		p.ResponseFormat = json.RawMessage(root.Get("response_format").Raw)
	}
	if root.Get("reasoning").Exists() {
		p.Reasoning = json.RawMessage(root.Get("reasoning").Raw)
	} else if root.Get("thinking").Exists() {
		p.Reasoning = json.RawMessage(root.Get("thinking").Raw)
	}

	switch proto {
	case ChatCompletions:
		p.Messages = json.RawMessage(root.Get("messages").Raw)
		p.System, p.Developer = splitSystem(p.Messages)
	case Messages:
		if root.Get("system").Exists() {
			p.System = json.RawMessage(root.Get("system").Raw)
		}
		p.Messages = json.RawMessage(root.Get("messages").Raw)
	case Responses:
		if root.Get("instructions").Exists() {
			p.System = json.RawMessage(root.Get("instructions").Raw)
		}
		if root.Get("input").Exists() {
			p.Messages = json.RawMessage(root.Get("input").Raw)
		}
	}
	p.ProviderParams = extraParams(root)
	return p, nil
}

func splitSystem(messages json.RawMessage) (sys, dev json.RawMessage) {
	var sysParts, devParts []json.RawMessage
	gjson.ParseBytes(messages).ForEach(func(_, msg gjson.Result) bool {
		switch msg.Get("role").String() {
		case "system":
			sysParts = append(sysParts, json.RawMessage(msg.Raw))
		case "developer":
			devParts = append(devParts, json.RawMessage(msg.Raw))
		}
		return true
	})
	if len(sysParts) > 0 {
		b, _ := json.Marshal(sysParts)
		sys = b
	}
	if len(devParts) > 0 {
		b, _ := json.Marshal(devParts)
		dev = b
	}
	return sys, dev
}

func extraParams(root gjson.Result) json.RawMessage {
	skip := map[string]bool{
		"model": true, "messages": true, "input": true, "instructions": true,
		"stream": true, "stream_options": true, "temperature": true, "top_p": true,
		"max_tokens": true, "max_output_tokens": true, "max_completion_tokens": true,
		"seed": true, "tools": true, "tool_choice": true, "response_format": true,
		"reasoning": true, "thinking": true, "system": true, "previous_response_id": true,
	}
	extras := map[string]json.RawMessage{}
	root.ForEach(func(k, v gjson.Result) bool {
		if !skip[k.String()] {
			extras[k.String()] = json.RawMessage(v.Raw)
		}
		return true
	})
	if len(extras) == 0 {
		return nil
	}
	b, _ := json.Marshal(extras)
	return b
}

func (p *ParsedRequest) ToCanonical(tenant, provider, model string) canonical.Request {
	return canonical.Request{
		SchemaVersion:    canonical.Schema,
		Tenant:           tenant,
		Protocol:         string(p.Protocol),
		Provider:         provider,
		Model:            model,
		Messages:         p.Messages,
		SystemPrompt:     p.System,
		DeveloperPrompt:  p.Developer,
		Temperature:      p.Temperature,
		TopP:             p.TopP,
		MaxTokens:        p.MaxTokens,
		Seed:             p.Seed,
		ResponseFormat:   p.ResponseFormat,
		Tools:            p.Tools,
		ToolChoice:       p.ToolChoice,
		Reasoning:        p.Reasoning,
		ProviderParams:   p.ProviderParams,
		MultimodalHashes: canonical.MultimodalHashes(p.Messages),
		PreviousResponse: p.PreviousResponse,
		HasTools:         p.HasTools,
	}
}
