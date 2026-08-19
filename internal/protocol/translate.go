package protocol

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// SetModel replaces only the model field so native hops keep original key order and message bytes.
func SetModel(raw []byte, model string) ([]byte, error) {
	return sjson.SetBytes(raw, "model", model)
}

func TranslateRequest(raw []byte, from, to Protocol, upstreamModel string) ([]byte, error) {
	if from == to {
		return SetModel(raw, upstreamModel)
	}
	parsed, err := Parse(from, raw)
	if err != nil {
		return nil, err
	}
	switch {
	case from == ChatCompletions && to == Messages:
		return openaiChatToAnthropic(parsed, upstreamModel)
	case from == Messages && to == ChatCompletions:
		return anthropicToOpenAIChat(parsed, upstreamModel)
	case from == ChatCompletions && to == Responses:
		return openaiChatToResponses(parsed, upstreamModel)
	case from == Responses && to == ChatCompletions:
		return responsesToOpenAIChat(parsed, upstreamModel)
	case from == Messages && to == Responses:
		chat, err := anthropicToOpenAIChat(parsed, upstreamModel)
		if err != nil {
			return nil, err
		}
		p2, err := Parse(ChatCompletions, chat)
		if err != nil {
			return nil, err
		}
		return openaiChatToResponses(p2, upstreamModel)
	case from == Responses && to == Messages:
		chat, err := responsesToOpenAIChat(parsed, upstreamModel)
		if err != nil {
			return nil, err
		}
		p2, err := Parse(ChatCompletions, chat)
		if err != nil {
			return nil, err
		}
		return openaiChatToAnthropic(p2, upstreamModel)
	default:
		return SetModel(raw, upstreamModel)
	}
}

func openaiChatToAnthropic(p *ParsedRequest, model string) ([]byte, error) {
	out := map[string]any{
		"model":      model,
		"max_tokens": 4096,
		"messages":   []any{},
	}
	if p.MaxTokens != nil {
		out["max_tokens"] = *p.MaxTokens
	}
	if p.Temperature != nil {
		out["temperature"] = *p.Temperature
	}
	if p.TopP != nil {
		out["top_p"] = *p.TopP
	}
	if p.Stream {
		out["stream"] = true
	}
	var systemParts []string
	var msgs []any
	gjson.ParseBytes(p.Messages).ForEach(func(_, msg gjson.Result) bool {
		role := msg.Get("role").String()
		switch role {
		case "system", "developer":
			systemParts = append(systemParts, textOf(msg))
		case "user":
			msgs = append(msgs, map[string]any{"role": "user", "content": contentToAnthropic(msg, false)})
		case "assistant":
			am := map[string]any{"role": "assistant", "content": contentToAnthropic(msg, true)}
			msgs = append(msgs, am)
		case "tool":
			block := map[string]any{
				"type":        "tool_result",
				"tool_use_id": msg.Get("tool_call_id").String(),
				"content":     textOf(msg),
			}
			if len(msgs) > 0 {
				last, _ := msgs[len(msgs)-1].(map[string]any)
				if last["role"] == "user" {
					if arr, ok := last["content"].([]any); ok {
						last["content"] = append(arr, block)
						return true
					}
				}
			}
			msgs = append(msgs, map[string]any{"role": "user", "content": []any{block}})
		}
		return true
	})
	if len(systemParts) > 0 {
		out["system"] = strings.Join(systemParts, "\n")
	}
	out["messages"] = msgs
	if p.HasTools {
		var tools []any
		gjson.ParseBytes(p.Tools).ForEach(func(_, t gjson.Result) bool {
			fn := t.Get("function")
			name := fn.Get("name").String()
			if name == "" {
				name = t.Get("name").String()
			}
			schema := fn.Get("parameters").Value()
			if schema == nil {
				schema = t.Get("input_schema").Value()
			}
			tools = append(tools, map[string]any{
				"name":         name,
				"description":  firstNonEmpty(fn.Get("description").String(), t.Get("description").String()),
				"input_schema": schema,
			})
			return true
		})
		out["tools"] = tools
		if len(p.ToolChoice) > 0 {
			out["tool_choice"] = openaiToolChoiceToAnthropic(p.ToolChoice)
		}
	}
	return json.Marshal(out)
}

func anthropicToOpenAIChat(p *ParsedRequest, model string) ([]byte, error) {
	var msgs []any
	if len(p.System) > 0 {
		sys := gjson.ParseBytes(p.System)
		text := sys.String()
		if sys.IsArray() {
			var parts []string
			sys.ForEach(func(_, el gjson.Result) bool {
				if el.Get("text").Exists() {
					parts = append(parts, el.Get("text").String())
				} else if el.Type == gjson.String {
					parts = append(parts, el.String())
				}
				return true
			})
			text = strings.Join(parts, "\n")
		} else if sys.IsObject() {
			text = sys.Get("text").String()
		}
		if strings.Trim(text, `"`) != "" {
			msgs = append(msgs, map[string]any{"role": "system", "content": strings.Trim(text, `"`)})
		}
	}
	gjson.ParseBytes(p.Messages).ForEach(func(_, msg gjson.Result) bool {
		role := msg.Get("role").String()
		content := msg.Get("content")
		if content.Type == gjson.String {
			msgs = append(msgs, map[string]any{"role": role, "content": content.String()})
			return true
		}
		var text []string
		var toolCalls []any
		var toolResults []any
		content.ForEach(func(_, part gjson.Result) bool {
			switch part.Get("type").String() {
			case "text":
				text = append(text, part.Get("text").String())
			case "tool_use":
				args, _ := json.Marshal(part.Get("input").Value())
				toolCalls = append(toolCalls, map[string]any{
					"id":   part.Get("id").String(),
					"type": "function",
					"function": map[string]any{
						"name":      part.Get("name").String(),
						"arguments": string(args),
					},
				})
			case "tool_result":
				toolResults = append(toolResults, map[string]any{
					"role":         "tool",
					"tool_call_id": part.Get("tool_use_id").String(),
					"content":      part.Get("content").String(),
				})
			}
			return true
		})
		if role == "assistant" || (len(text) > 0 || len(toolCalls) > 0) {
			m := map[string]any{"role": role, "content": strings.Join(text, "")}
			if len(toolCalls) > 0 {
				m["tool_calls"] = toolCalls
			}
			msgs = append(msgs, m)
		}
		msgs = append(msgs, toolResults...)
		return true
	})
	out := map[string]any{"model": model, "messages": msgs}
	if p.Temperature != nil {
		out["temperature"] = *p.Temperature
	}
	if p.TopP != nil {
		out["top_p"] = *p.TopP
	}
	if p.MaxTokens != nil {
		out["max_tokens"] = *p.MaxTokens
	}
	if p.Stream {
		out["stream"] = true
	}
	if p.HasTools {
		var tools []any
		gjson.ParseBytes(p.Tools).ForEach(func(_, t gjson.Result) bool {
			schema := t.Get("input_schema").Value()
			if schema == nil {
				schema = t.Get("function.parameters").Value()
			}
			name := t.Get("name").String()
			if name == "" {
				name = t.Get("function.name").String()
			}
			tools = append(tools, map[string]any{
				"type": "function",
				"function": map[string]any{
					"name":        name,
					"description": t.Get("description").String(),
					"parameters":  schema,
				},
			})
			return true
		})
		out["tools"] = tools
	}
	return json.Marshal(out)
}

func openaiChatToResponses(p *ParsedRequest, model string) ([]byte, error) {
	out := map[string]any{"model": model}
	if p.Stream {
		out["stream"] = true
	}
	if p.Temperature != nil {
		out["temperature"] = *p.Temperature
	}
	if p.MaxTokens != nil {
		out["max_output_tokens"] = *p.MaxTokens
	}
	var instructions []string
	var input []any
	gjson.ParseBytes(p.Messages).ForEach(func(_, msg gjson.Result) bool {
		role := msg.Get("role").String()
		if role == "system" || role == "developer" {
			instructions = append(instructions, textOf(msg))
			return true
		}
		item := map[string]any{"role": role, "content": textOf(msg)}
		input = append(input, item)
		return true
	})
	if len(instructions) > 0 {
		out["instructions"] = strings.Join(instructions, "\n")
	}
	out["input"] = input
	if p.HasTools {
		out["tools"] = gjson.ParseBytes(p.Tools).Value()
	}
	if p.PreviousResponse != "" {
		out["previous_response_id"] = p.PreviousResponse
	}
	return json.Marshal(out)
}

func responsesToOpenAIChat(p *ParsedRequest, model string) ([]byte, error) {
	var msgs []any
	if len(p.System) > 0 {
		txt := strings.Trim(string(p.System), `"`)
		if gjson.ValidBytes(p.System) && gjson.ParseBytes(p.System).Type == gjson.String {
			txt = gjson.ParseBytes(p.System).String()
		}
		msgs = append(msgs, map[string]any{"role": "system", "content": txt})
	}
	in := gjson.ParseBytes(p.Messages)
	if in.Type == gjson.String {
		msgs = append(msgs, map[string]any{"role": "user", "content": in.String()})
	} else if in.IsArray() {
		in.ForEach(func(_, el gjson.Result) bool {
			role := el.Get("role").String()
			if role == "" {
				role = "user"
			}
			msgs = append(msgs, map[string]any{"role": role, "content": textOf(el)})
			return true
		})
	}
	out := map[string]any{"model": model, "messages": msgs}
	if p.Temperature != nil {
		out["temperature"] = *p.Temperature
	}
	if p.MaxTokens != nil {
		out["max_tokens"] = *p.MaxTokens
	}
	if p.Stream {
		out["stream"] = true
	}
	if p.HasTools {
		out["tools"] = gjson.ParseBytes(p.Tools).Value()
	}
	return json.Marshal(out)
}

func contentToAnthropic(msg gjson.Result, assistant bool) any {
	var blocks []any
	if msg.Get("content").Type == gjson.String {
		if s := msg.Get("content").String(); s != "" {
			blocks = append(blocks, map[string]any{"type": "text", "text": s})
		}
	} else if msg.Get("content").IsArray() {
		msg.Get("content").ForEach(func(_, part gjson.Result) bool {
			if part.Get("type").String() == "text" || part.Get("text").Exists() {
				blocks = append(blocks, map[string]any{"type": "text", "text": part.Get("text").String()})
			}
			return true
		})
	}
	if assistant {
		msg.Get("tool_calls").ForEach(func(_, tc gjson.Result) bool {
			var input any
			args := tc.Get("function.arguments").String()
			if args != "" && json.Valid([]byte(args)) {
				_ = json.Unmarshal([]byte(args), &input)
			} else {
				input = map[string]any{}
			}
			blocks = append(blocks, map[string]any{
				"type":  "tool_use",
				"id":    tc.Get("id").String(),
				"name":  tc.Get("function.name").String(),
				"input": input,
			})
			return true
		})
	}
	if len(blocks) == 0 {
		return []any{map[string]any{"type": "text", "text": ""}}
	}
	return blocks
}

func openaiToolChoiceToAnthropic(raw json.RawMessage) any {
	s := strings.TrimSpace(string(raw))
	s = strings.Trim(s, `"`)
	switch s {
	case "auto":
		return map[string]any{"type": "auto"}
	case "none":
		return map[string]any{"type": "none"}
	case "required", "any":
		return map[string]any{"type": "any"}
	}
	name := gjson.GetBytes(raw, "function.name").String()
	if name != "" {
		return map[string]any{"type": "tool", "name": name}
	}
	return map[string]any{"type": "auto"}
}

func textOf(msg gjson.Result) string {
	c := msg.Get("content")
	if c.Type == gjson.String {
		return c.String()
	}
	if c.IsArray() {
		var b strings.Builder
		c.ForEach(func(_, part gjson.Result) bool {
			if part.Get("text").Exists() {
				b.WriteString(part.Get("text").String())
			} else if part.Type == gjson.String {
				b.WriteString(part.String())
			}
			return true
		})
		return b.String()
	}
	if msg.Get("text").Exists() {
		return msg.Get("text").String()
	}
	return ""
}

func firstNonEmpty(vs ...string) string {
	for _, v := range vs {
		if v != "" {
			return v
		}
	}
	return ""
}

func FinishOpenAI(reason string) string {
	switch reason {
	case "end_turn", "stop":
		return "stop"
	case "max_tokens", "length":
		return "length"
	case "tool_use", "tool_calls":
		return "tool_calls"
	default:
		if reason == "" {
			return "stop"
		}
		return reason
	}
}

func FinishAnthropic(reason string) string {
	switch reason {
	case "stop", "end_turn":
		return "end_turn"
	case "length", "max_tokens":
		return "max_tokens"
	case "tool_calls", "tool_use":
		return "tool_use"
	default:
		if reason == "" {
			return "end_turn"
		}
		return reason
	}
}

func MustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		return []byte(fmt.Sprintf(`{"error":%q}`, err.Error()))
	}
	return b
}
