package canonical

import (
	"encoding/json"
	"testing"
)

func BenchmarkCacheKey(b *testing.B) {
	msg := json.RawMessage(`[{"role":"system","content":"large prefix"},{"role":"user","content":"if x {\n\treturn\n}"}]`)
	r := Request{Tenant: "t", Protocol: "chat_completions", Provider: "opencode_go", Model: "deepseek-v4-flash", Messages: msg}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _, _ = r.CacheKey("v1")
	}
}
