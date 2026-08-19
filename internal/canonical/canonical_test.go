package canonical

import (
	"encoding/json"
	"testing"
)

func TestStructuralJSONSortsKeysOnly(t *testing.T) {
	in := []byte(`{"b":2,"a":{"z":1,"y":"  keep  spaces  "}}`)
	out, err := StructuralJSON(in)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != `{"a":{"y":"  keep  spaces  ","z":1},"b":2}` {
		t.Fatalf("got %s", out)
	}
}

func TestCacheKeyStableAndTenantIsolated(t *testing.T) {
	msg := json.RawMessage(`[{"role":"user","content":"func main() {\n\tfmt.Println(1)\n}"}]`)
	a := Request{Tenant: "t1", Protocol: "chat_completions", Provider: "opencode_go", Model: "deepseek-v4-flash", Messages: msg}
	b := Request{Tenant: "t1", Protocol: "chat_completions", Provider: "opencode_go", Model: "deepseek-v4-flash", Messages: msg}
	c := Request{Tenant: "t2", Protocol: "chat_completions", Provider: "opencode_go", Model: "deepseek-v4-flash", Messages: msg}
	ka, ha, err := a.CacheKey("v1")
	if err != nil {
		t.Fatal(err)
	}
	kb, hb, err := b.CacheKey("v1")
	if err != nil {
		t.Fatal(err)
	}
	kc, hc, err := c.CacheKey("v1")
	if err != nil {
		t.Fatal(err)
	}
	if ka != kb || ha != hb {
		t.Fatal("identical requests must share key")
	}
	if ka == kc || ha == hc {
		t.Fatal("tenants must not share keys")
	}
	if ka[:10] != "llmgw:v1:t" {
		t.Fatalf("key prefix %s", ka)
	}
}

func TestWhitespaceInCodeMatters(t *testing.T) {
	a := Request{Tenant: "t", Protocol: "chat_completions", Provider: "p", Model: "m", Messages: json.RawMessage(`[{"role":"user","content":"if x {\n\treturn\n}"}]`)}
	b := Request{Tenant: "t", Protocol: "chat_completions", Provider: "p", Model: "m", Messages: json.RawMessage(`[{"role":"user","content":"if x {\n  return\n}"}]`)}
	_, ha, _ := a.CacheKey("v1")
	_, hb, _ := b.CacheKey("v1")
	if ha == hb {
		t.Fatal("indentation must change the key")
	}
}

func TestPrefixIgnoresLastMessage(t *testing.T) {
	m1 := json.RawMessage(`[{"role":"system","content":"S"},{"role":"user","content":"old"},{"role":"user","content":"new1"}]`)
	m2 := json.RawMessage(`[{"role":"system","content":"S"},{"role":"user","content":"old"},{"role":"user","content":"new2"}]`)
	p1 := Prefix(m1, nil, json.RawMessage(`[{"role":"system","content":"S"}]`))
	p2 := Prefix(m2, nil, json.RawMessage(`[{"role":"system","content":"S"}]`))
	if p1.Hash != p2.Hash {
		t.Fatal("shared prefix should match")
	}
	if p1.Length == 0 {
		t.Fatal("prefix length")
	}
}
