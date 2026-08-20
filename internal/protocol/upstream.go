package protocol

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"strings"

	"github.com/tidwall/gjson"
)

func ParseUpstreamCompletion(proto Protocol, body []byte) (Completion, error) {
	root := gjson.ParseBytes(body)
	c := Completion{Created: NowUnix()}
	switch proto {
	case Messages:
		c.ID = root.Get("id").String()
		c.Model = root.Get("model").String()
		c.FinishReason = root.Get("stop_reason").String()
		root.Get("content").ForEach(func(_, part gjson.Result) bool {
			switch part.Get("type").String() {
			case "text":
				c.Content += part.Get("text").String()
			case "thinking":
				c.Reasoning += part.Get("thinking").String()
			case "tool_use":
				args, _ := json.Marshal(part.Get("input").Value())
				c.ToolCalls = append(c.ToolCalls, ToolCall{
					ID: part.Get("id").String(), Name: part.Get("name").String(), Arguments: string(args),
				})
			}
			return true
		})
		c.Usage = Usage{
			InputTokens:       int(root.Get("usage.input_tokens").Int()),
			OutputTokens:      int(root.Get("usage.output_tokens").Int()),
			CachedInputTokens: int(firstInt(root, "usage.cache_read_input_tokens", "usage.cache_read_tokens")),
			CacheWriteTokens:  int(firstInt(root, "usage.cache_creation_input_tokens", "usage.cache_creation_tokens")),
		}
	case Responses:
		c.ID = root.Get("id").String()
		c.Model = root.Get("model").String()
		c.FinishReason = root.Get("status").String()
		root.Get("output").ForEach(func(_, item gjson.Result) bool {
			switch item.Get("type").String() {
			case "message":
				item.Get("content").ForEach(func(_, part gjson.Result) bool {
					c.Content += part.Get("text").String()
					return true
				})
			case "function_call":
				c.ToolCalls = append(c.ToolCalls, ToolCall{
					ID: item.Get("call_id").String(), Name: item.Get("name").String(), Arguments: item.Get("arguments").String(),
				})
			case "reasoning":
				c.Reasoning += item.Get("summary.0.text").String()
			}
			return true
		})
		c.Usage = Usage{
			InputTokens:       int(root.Get("usage.input_tokens").Int()),
			OutputTokens:      int(root.Get("usage.output_tokens").Int()),
			CachedInputTokens: int(root.Get("usage.input_tokens_details.cached_tokens").Int()),
			ReasoningTokens:   int(root.Get("usage.output_tokens_details.reasoning_tokens").Int()),
		}
	default:
		c.ID = root.Get("id").String()
		c.Model = root.Get("model").String()
		c.Created = root.Get("created").Int()
		ch := root.Get("choices.0")
		c.Content = ch.Get("message.content").String()
		c.Reasoning = firstString(ch, "message.reasoning", "message.reasoning_content")
		c.FinishReason = ch.Get("finish_reason").String()
		ch.Get("message.tool_calls").ForEach(func(_, tc gjson.Result) bool {
			c.ToolCalls = append(c.ToolCalls, ToolCall{
				ID: tc.Get("id").String(), Name: tc.Get("function.name").String(), Arguments: tc.Get("function.arguments").String(),
			})
			return true
		})
		c.Usage = Usage{
			InputTokens:       int(root.Get("usage.prompt_tokens").Int()),
			OutputTokens:      int(root.Get("usage.completion_tokens").Int()),
			CachedInputTokens: int(firstInt(root, "usage.prompt_tokens_details.cached_tokens", "usage.prompt_cache_hit_tokens")),
			CacheWriteTokens: int(firstInt(root, "usage.cache_creation_input_tokens", "usage.prompt_tokens_details.cache_creation_tokens", "usage.prompt_cache_miss_tokens")),
			ReasoningTokens:   int(root.Get("usage.completion_tokens_details.reasoning_tokens").Int()),
		}
	}
	c.Usage.TotalTokens = c.Usage.InputTokens + c.Usage.OutputTokens
	return c, nil
}

func ConsumeSSE(r io.Reader, proto Protocol, onEvent func(StreamEvent) error) (Completion, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	var evName string
	var data bytes.Buffer
	var acc Completion
	acc.Created = NowUnix()
	flush := func() error {
		if data.Len() == 0 {
			evName = ""
			return nil
		}
		payload := bytes.TrimSpace(data.Bytes())
		data.Reset()
		if bytes.Equal(payload, []byte("[DONE]")) {
			_ = onEvent(StreamEvent{Done: true})
			evName = ""
			return nil
		}
		se := parseSSEPayload(proto, evName, payload, &acc)
		evName = ""
		if se.Err != nil {
			return se.Err
		}
		return onEvent(se)
	}
	for sc.Scan() {
		line := sc.Text()
		if line == "" {
			if err := flush(); err != nil {
				return acc, err
			}
			continue
		}
		if strings.HasPrefix(line, "event:") {
			evName = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			continue
		}
		if strings.HasPrefix(line, "data:") {
			if data.Len() > 0 {
				data.WriteByte('\n')
			}
			data.WriteString(strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}
	if err := flush(); err != nil {
		return acc, err
	}
	if err := sc.Err(); err != nil {
		return acc, err
	}
	return acc, nil
}

func parseSSEPayload(proto Protocol, ev string, payload []byte, acc *Completion) StreamEvent {
	root := gjson.ParseBytes(payload)
	se := StreamEvent{Event: ev, Raw: append([]byte(nil), payload...)}
	if root.Get("error").Exists() && root.Get("error").Type != gjson.Null {
		se.Err = io.ErrUnexpectedEOF
		return se
	}
	switch proto {
	case Messages:
		switch ev {
		case "content_block_delta":
			if root.Get("delta.type").String() == "thinking_delta" {
				t := root.Get("delta.thinking").String()
				acc.Reasoning += t
				se.DeltaReasoning = t
			} else if root.Get("delta.text").Exists() {
				t := root.Get("delta.text").String()
				acc.Content += t
				se.DeltaContent = t
			} else if root.Get("delta.partial_json").Exists() {
				// accumulate last tool args
				if n := len(acc.ToolCalls); n > 0 {
					acc.ToolCalls[n-1].Arguments += root.Get("delta.partial_json").String()
					se.ToolCalls = []ToolCall{acc.ToolCalls[n-1]}
				}
			}
		case "content_block_start":
			if root.Get("content_block.type").String() == "tool_use" {
				acc.ToolCalls = append(acc.ToolCalls, ToolCall{
					ID: root.Get("content_block.id").String(), Name: root.Get("content_block.name").String(),
				})
			}
		case "message_start":
			acc.ID = root.Get("message.id").String()
			acc.Model = root.Get("message.model").String()
			acc.Usage.InputTokens = int(root.Get("message.usage.input_tokens").Int())
			acc.Usage.CachedInputTokens = int(root.Get("message.usage.cache_read_input_tokens").Int())
		case "message_delta":
			acc.FinishReason = root.Get("delta.stop_reason").String()
			acc.Usage.OutputTokens = int(root.Get("usage.output_tokens").Int())
			u := acc.Usage
			se.Usage = &u
			se.FinishReason = acc.FinishReason
		case "message_stop":
			se.Done = true
		}
	case Responses:
		switch ev {
		case "response.output_text.delta":
			t := root.Get("delta").String()
			acc.Content += t
			se.DeltaContent = t
		case "response.completed":
			if comp, err := ParseUpstreamCompletion(Responses, []byte(root.Get("response").Raw)); err == nil {
				*acc = comp
				se.Usage = &acc.Usage
				se.FinishReason = acc.FinishReason
			}
			se.Done = true
		}
		if root.Get("response.id").Exists() {
			acc.ID = root.Get("response.id").String()
			acc.Model = root.Get("response.model").String()
		}
	default:
		acc.ID = firstNonEmpty(acc.ID, root.Get("id").String())
		acc.Model = firstNonEmpty(acc.Model, root.Get("model").String())
		delta := root.Get("choices.0.delta")
		if t := delta.Get("content").String(); t != "" {
			acc.Content += t
			se.DeltaContent = t
		}
		if t := firstString(delta, "reasoning", "reasoning_content"); t != "" {
			acc.Reasoning += t
			se.DeltaReasoning = t
		}
		delta.Get("tool_calls").ForEach(func(_, tc gjson.Result) bool {
			idx := int(tc.Get("index").Int())
			for len(acc.ToolCalls) <= idx {
				acc.ToolCalls = append(acc.ToolCalls, ToolCall{})
			}
			if id := tc.Get("id").String(); id != "" {
				acc.ToolCalls[idx].ID = id
			}
			if n := tc.Get("function.name").String(); n != "" {
				acc.ToolCalls[idx].Name = n
			}
			acc.ToolCalls[idx].Arguments += tc.Get("function.arguments").String()
			se.ToolCalls = []ToolCall{acc.ToolCalls[idx]}
			return true
		})
		if fr := root.Get("choices.0.finish_reason"); fr.Exists() && fr.Type != gjson.Null {
			acc.FinishReason = fr.String()
			se.FinishReason = acc.FinishReason
		}
		if root.Get("usage").Exists() && root.Get("usage").Type != gjson.Null {
			u := Usage{
				InputTokens:       int(root.Get("usage.prompt_tokens").Int()),
				OutputTokens:      int(root.Get("usage.completion_tokens").Int()),
				CachedInputTokens: int(firstInt(root, "usage.prompt_tokens_details.cached_tokens", "usage.prompt_cache_hit_tokens")),
				ReasoningTokens:   int(root.Get("usage.completion_tokens_details.reasoning_tokens").Int()),
			}
			acc.Usage = u
			se.Usage = &u
		}
	}
	acc.Usage.TotalTokens = acc.Usage.InputTokens + acc.Usage.OutputTokens
	return se
}

func firstInt(root gjson.Result, paths ...string) int64 {
	for _, p := range paths {
		if root.Get(p).Exists() {
			return root.Get(p).Int()
		}
	}
	return 0
}

func firstString(root gjson.Result, paths ...string) string {
	for _, p := range paths {
		if s := root.Get(p).String(); s != "" {
			return s
		}
	}
	return ""
}
