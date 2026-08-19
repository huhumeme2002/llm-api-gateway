package protocol

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

func EncodeCompletion(proto Protocol, c Completion, requestModel string) []byte {
	if requestModel != "" {
		c.Model = requestModel
	}
	switch proto {
	case Messages:
		return encodeAnthropic(c)
	case Responses:
		return encodeResponses(c)
	default:
		return encodeOpenAI(c)
	}
}

func encodeOpenAI(c Completion) []byte {
	msg := map[string]any{"role": "assistant", "content": c.Content}
	if len(c.ToolCalls) > 0 {
		var tcs []any
		for _, t := range c.ToolCalls {
			tcs = append(tcs, map[string]any{
				"id": t.ID, "type": "function",
				"function": map[string]any{"name": t.Name, "arguments": t.Arguments},
			})
		}
		msg["tool_calls"] = tcs
		msg["content"] = nilIfEmpty(c.Content)
	}
	body := map[string]any{
		"id":      nonEmpty(c.ID, "chatcmpl-"+c.ID),
		"object":  "chat.completion",
		"created": c.Created,
		"model":   c.Model,
		"choices": []any{map[string]any{
			"index": 0, "message": msg, "finish_reason": FinishOpenAI(c.FinishReason),
		}},
		"usage": openaiUsage(c.Usage),
	}
	if c.Reasoning != "" {
		body["choices"].([]any)[0].(map[string]any)["message"].(map[string]any)["reasoning"] = c.Reasoning
	}
	return MustJSON(body)
}

func encodeAnthropic(c Completion) []byte {
	var content []any
	if c.Content != "" {
		content = append(content, map[string]any{"type": "text", "text": c.Content})
	}
	for _, t := range c.ToolCalls {
		var input any
		if json.Valid([]byte(t.Arguments)) {
			_ = json.Unmarshal([]byte(t.Arguments), &input)
		}
		content = append(content, map[string]any{
			"type": "tool_use", "id": t.ID, "name": t.Name, "input": input,
		})
	}
	if len(content) == 0 {
		content = []any{map[string]any{"type": "text", "text": ""}}
	}
	id := c.ID
	if !strings.HasPrefix(id, "msg_") {
		id = "msg_" + strings.TrimPrefix(id, "chatcmpl-")
	}
	body := map[string]any{
		"id":            id,
		"type":          "message",
		"role":          "assistant",
		"model":         c.Model,
		"content":       content,
		"stop_reason":   FinishAnthropic(c.FinishReason),
		"stop_sequence": nil,
		"usage": map[string]any{
			"input_tokens":                c.Usage.InputTokens,
			"output_tokens":               c.Usage.OutputTokens,
			"cache_read_input_tokens":     c.Usage.CachedInputTokens,
			"cache_creation_input_tokens": c.Usage.CacheWriteTokens,
		},
	}
	return MustJSON(body)
}

func encodeResponses(c Completion) []byte {
	id := c.ID
	if !strings.HasPrefix(id, "resp_") {
		id = "resp_" + strings.TrimPrefix(id, "chatcmpl-")
	}
	var output []any
	if c.Content != "" {
		output = append(output, map[string]any{
			"type": "message", "role": "assistant",
			"content": []any{map[string]any{"type": "output_text", "text": c.Content}},
		})
	}
	for _, t := range c.ToolCalls {
		output = append(output, map[string]any{
			"type": "function_call", "call_id": t.ID, "name": t.Name, "arguments": t.Arguments,
		})
	}
	body := map[string]any{
		"id":     id,
		"object": "response",
		"status": "completed",
		"model":  c.Model,
		"output": output,
		"usage": map[string]any{
			"input_tokens":  c.Usage.InputTokens,
			"output_tokens": c.Usage.OutputTokens,
			"input_tokens_details": map[string]any{
				"cached_tokens": c.Usage.CachedInputTokens,
			},
		},
	}
	return MustJSON(body)
}

func WriteStream(w io.Writer, proto Protocol, c Completion, requestModel string, flusher func()) error {
	if requestModel != "" {
		c.Model = requestModel
	}
	switch proto {
	case Messages:
		return writeAnthropicStream(w, c, flusher)
	case Responses:
		return writeResponsesStream(w, c, flusher)
	default:
		return writeOpenAIStream(w, c, flusher)
	}
}

func writeOpenAIStream(w io.Writer, c Completion, flush func()) error {
	id := nonEmpty(c.ID, "chatcmpl-cached")
	chunk := func(delta map[string]any, finish any, usage any) error {
		body := map[string]any{
			"id": id, "object": "chat.completion.chunk", "created": c.Created, "model": c.Model,
			"choices": []any{map[string]any{"index": 0, "delta": delta, "finish_reason": finish}},
		}
		if usage != nil {
			body["usage"] = usage
		}
		if _, err := fmt.Fprintf(w, "data: %s\n\n", MustJSON(body)); err != nil {
			return err
		}
		if flush != nil {
			flush()
		}
		return nil
	}
	if err := chunk(map[string]any{"role": "assistant", "content": ""}, nil, nil); err != nil {
		return err
	}
	if c.Reasoning != "" {
		if err := chunk(map[string]any{"reasoning": c.Reasoning}, nil, nil); err != nil {
			return err
		}
	}
	if c.Content != "" {
		if err := chunk(map[string]any{"content": c.Content}, nil, nil); err != nil {
			return err
		}
	}
	if len(c.ToolCalls) > 0 {
		var tcs []any
		for i, t := range c.ToolCalls {
			tcs = append(tcs, map[string]any{
				"index": i, "id": t.ID, "type": "function",
				"function": map[string]any{"name": t.Name, "arguments": t.Arguments},
			})
		}
		if err := chunk(map[string]any{"tool_calls": tcs}, nil, nil); err != nil {
			return err
		}
	}
	if err := chunk(map[string]any{}, FinishOpenAI(c.FinishReason), openaiUsage(c.Usage)); err != nil {
		return err
	}
	_, err := io.WriteString(w, "data: [DONE]\n\n")
	if flush != nil {
		flush()
	}
	return err
}

func writeAnthropicStream(w io.Writer, c Completion, flush func()) error {
	id := c.ID
	if !strings.HasPrefix(id, "msg_") {
		id = "msg_" + strings.TrimPrefix(id, "chatcmpl-")
	}
	ev := func(name string, payload any) error {
		if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", name, MustJSON(payload)); err != nil {
			return err
		}
		if flush != nil {
			flush()
		}
		return nil
	}
	if err := ev("message_start", map[string]any{
		"type": "message_start",
		"message": map[string]any{
			"id": id, "type": "message", "role": "assistant", "model": c.Model,
			"content": []any{}, "stop_reason": nil,
			"usage": map[string]any{"input_tokens": c.Usage.InputTokens, "output_tokens": 0},
		},
	}); err != nil {
		return err
	}
	idx := 0
	if c.Reasoning != "" {
		if err := ev("content_block_start", map[string]any{
			"type": "content_block_start", "index": idx,
			"content_block": map[string]any{"type": "thinking", "thinking": ""},
		}); err != nil {
			return err
		}
		if err := ev("content_block_delta", map[string]any{
			"type": "content_block_delta", "index": idx,
			"delta": map[string]any{"type": "thinking_delta", "thinking": c.Reasoning},
		}); err != nil {
			return err
		}
		if err := ev("content_block_stop", map[string]any{"type": "content_block_stop", "index": idx}); err != nil {
			return err
		}
		idx++
	}
	if err := ev("content_block_start", map[string]any{
		"type": "content_block_start", "index": idx,
		"content_block": map[string]any{"type": "text", "text": ""},
	}); err != nil {
		return err
	}
	if c.Content != "" {
		if err := ev("content_block_delta", map[string]any{
			"type": "content_block_delta", "index": idx,
			"delta": map[string]any{"type": "text_delta", "text": c.Content},
		}); err != nil {
			return err
		}
	}
	if err := ev("content_block_stop", map[string]any{"type": "content_block_stop", "index": idx}); err != nil {
		return err
	}
	idx++
	for _, t := range c.ToolCalls {
		if err := ev("content_block_start", map[string]any{
			"type": "content_block_start", "index": idx,
			"content_block": map[string]any{"type": "tool_use", "id": t.ID, "name": t.Name, "input": map[string]any{}},
		}); err != nil {
			return err
		}
		if err := ev("content_block_delta", map[string]any{
			"type": "content_block_delta", "index": idx,
			"delta": map[string]any{"type": "input_json_delta", "partial_json": t.Arguments},
		}); err != nil {
			return err
		}
		if err := ev("content_block_stop", map[string]any{"type": "content_block_stop", "index": idx}); err != nil {
			return err
		}
		idx++
	}
	if err := ev("message_delta", map[string]any{
		"type":  "message_delta",
		"delta": map[string]any{"stop_reason": FinishAnthropic(c.FinishReason)},
		"usage": map[string]any{"output_tokens": c.Usage.OutputTokens},
	}); err != nil {
		return err
	}
	return ev("message_stop", map[string]any{"type": "message_stop"})
}

func writeResponsesStream(w io.Writer, c Completion, flush func()) error {
	id := c.ID
	if !strings.HasPrefix(id, "resp_") {
		id = "resp_" + strings.TrimPrefix(id, "chatcmpl-")
	}
	ev := func(name string, payload any) error {
		if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", name, MustJSON(payload)); err != nil {
			return err
		}
		if flush != nil {
			flush()
		}
		return nil
	}
	if err := ev("response.created", map[string]any{"type": "response.created", "response": map[string]any{"id": id, "model": c.Model, "status": "in_progress"}}); err != nil {
		return err
	}
	if c.Content != "" {
		if err := ev("response.output_text.delta", map[string]any{"type": "response.output_text.delta", "delta": c.Content}); err != nil {
			return err
		}
	}
	return ev("response.completed", map[string]any{
		"type": "response.completed",
		"response": json.RawMessage(encodeResponses(c)),
	})
}

func openaiUsage(u Usage) map[string]any {
	return map[string]any{
		"prompt_tokens":     u.InputTokens,
		"completion_tokens": u.OutputTokens,
		"total_tokens":      u.InputTokens + u.OutputTokens,
		"prompt_tokens_details": map[string]any{
			"cached_tokens": u.CachedInputTokens,
		},
		"completion_tokens_details": map[string]any{
			"reasoning_tokens": u.ReasoningTokens,
		},
	}
}

func nilIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nonEmpty(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}
