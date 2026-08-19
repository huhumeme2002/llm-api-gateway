package provider

import "testing"

func TestRedactSecrets(t *testing.T) {
	in := `dial socks5://user:s3cret@5.6.7.8:1080: timeout Authorization: Bearer sk-abcdefghijklmnop`
	out := Redact(in)
	if containsAny(out, "s3cret", "sk-abcdefghijklmnop") {
		t.Fatal(out)
	}
	if !containsAny(out, "***:***@", "[redacted]") {
		t.Fatal(out)
	}
}

func containsAny(s string, parts ...string) bool {
	for _, p := range parts {
		if len(p) > 0 && (len(s) >= len(p)) {
			for i := 0; i+len(p) <= len(s); i++ {
				if s[i:i+len(p)] == p {
					return true
				}
			}
		}
	}
	return false
}
