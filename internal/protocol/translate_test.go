package protocol

import (
	"encoding/json"
	"testing"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

func TestSetModelPreservesBytes(t *testing.T) {
	raw := []byte(`{"messages":[{"role":"user","content":"if x {\n\treturn\n}"}],"model":"fast","temperature":0}`)
	out, err := SetModel(raw, "deepseek-v4-flash")
	if err != nil {
		t.Fatal(err)
	}
	if gjson.GetBytes(out, "messages.0.content").String() != gjson.GetBytes(raw, "messages.0.content").String() {
		t.Fatalf("content rewritten: %s", out)
	}
	if gjson.GetBytes(out, "model").String() != "deepseek-v4-flash" {
		t.Fatal(string(out))
	}
}

func TestOpenAIToAnthropicTools(t *testing.T) {
	raw := []byte(`{
		"model":"m",
		"messages":[
			{"role":"system","content":"sys"},
			{"role":"user","content":"hi"},
			{"role":"assistant","content":null,"tool_calls":[{"id":"c1","type":"function","function":{"name":"read","arguments":"{\"path\":\"a.go\"}"}}]},
			{"role":"tool","tool_call_id":"c1","content":"ok"}
		],
		"tools":[{"type":"function","function":{"name":"read","description":"r","parameters":{"type":"object","properties":{"path":{"type":"string"}}}}}],
		"temperature":0,
		"max_tokens":16
	}`)
	p, err := Parse(ChatCompletions, raw)
	if err != nil {
		t.Fatal(err)
	}
	out, err := openaiChatToAnthropic(p, "minimax-m3")
	if err != nil {
		t.Fatal(err)
	}
	if gjson.GetBytes(out, "system").String() != "sys" {
		t.Fatalf("system: %s", out)
	}
	if gjson.GetBytes(out, "tools.0.name").String() != "read" {
		t.Fatalf("tools: %s", out)
	}
	if gjson.GetBytes(out, "tools.0.input_schema.properties.path.type").String() != "string" {
		t.Fatal("schema lost")
	}
	foundTool := false
	gjson.GetBytes(out, "messages").ForEach(func(_, m gjson.Result) bool {
		m.Get("content").ForEach(func(_, c gjson.Result) bool {
			if c.Get("type").String() == "tool_use" && c.Get("name").String() == "read" {
				foundTool = true
			}
			return true
		})
		return true
	})
	if !foundTool {
		t.Fatalf("tool_use missing: %s", out)
	}
}

func TestAnthropicToOpenAIRoundTripText(t *testing.T) {
	raw := []byte(`{"model":"claude-sonnet-4-6","max_tokens":32,"system":"S","messages":[{"role":"user","content":[{"type":"text","text":"hello"}]}]}`)
	p, err := Parse(Messages, raw)
	if err != nil {
		t.Fatal(err)
	}
	out, err := anthropicToOpenAIChat(p, "claude-sonnet-4-6")
	if err != nil {
		t.Fatal(err)
	}
	if gjson.GetBytes(out, "messages.0.role").String() != "system" {
		t.Fatal(string(out))
	}
	if gjson.GetBytes(out, "messages.1.content").String() != "hello" {
		t.Fatal(string(out))
	}
}

func TestTranslateRequestSameProtocolUsesSjson(t *testing.T) {
	raw := []byte(`{"model":"fast","messages":[{"role":"user","content":"x"}]}`)
	out, err := TranslateRequest(raw, ChatCompletions, ChatCompletions, "m1")
	if err != nil {
		t.Fatal(err)
	}
	want, _ := sjson.SetBytes(raw, "model", "m1")
	if string(out) != string(want) {
		t.Fatalf("%s vs %s", out, want)
	}
}

func TestEncodeOpenAIAndAnthropic(t *testing.T) {
	c := Completion{ID: "x", Model: "m", Created: 1, Content: "hi", FinishReason: "stop", Usage: Usage{InputTokens: 3, OutputTokens: 2}}
	oai := EncodeCompletion(ChatCompletions, c, "alias")
	if gjson.GetBytes(oai, "choices.0.message.content").String() != "hi" {
		t.Fatal(string(oai))
	}
	if gjson.GetBytes(oai, "model").String() != "alias" {
		t.Fatal(string(oai))
	}
	ant := EncodeCompletion(Messages, c, "alias")
	if gjson.GetBytes(ant, "content.0.text").String() != "hi" {
		t.Fatal(string(ant))
	}
}

func TestJSONValidAfterTranslate(t *testing.T) {
	raw := []byte(`{"model":"m","messages":[{"role":"user","content":"q"}],"temperature":0}`)
	for _, to := range []Protocol{Messages, Responses} {
		b, err := TranslateRequest(raw, ChatCompletions, to, "up")
		if err != nil {
			t.Fatal(err)
		}
		if !json.Valid(b) {
			t.Fatalf("%s: %s", to, b)
		}
	}
}
