package config

import "testing"

func TestResolvedCredentialsLegacyDefault(t *testing.T) {
	p := ProviderConfig{APIKeyEnv: "OPENCODE_GO_API_KEY"}
	cs := p.ResolvedCredentials()
	if len(cs) != 1 || cs[0].ID != "default" || cs[0].APIKeyEnv != "OPENCODE_GO_API_KEY" {
		t.Fatalf("%+v", cs)
	}
}

func TestResolvedCredentialsExplicit(t *testing.T) {
	p := ProviderConfig{
		APIKeyEnv: "IGNORE",
		Credentials: []CredentialConfig{
			{ID: "go-01", APIKeyEnv: "OPENCODE_GO_KEY_01", ProxyEnv: "OPENCODE_GO_PROXY_01"},
			{APIKeyEnv: "OPENCODE_GO_KEY_02"},
		},
	}
	cs := p.ResolvedCredentials()
	if len(cs) != 2 || cs[0].ID != "go-01" || cs[1].ID != "default" {
		t.Fatalf("%+v", cs)
	}
}

func TestParseMemLimit(t *testing.T) {
	n, err := ParseMemLimit("384MiB")
	if err != nil || n != 384<<20 {
		t.Fatalf("%d %v", n, err)
	}
	n, err = ParseMemLimit("1GiB")
	if err != nil || n != 1<<30 {
		t.Fatalf("%d %v", n, err)
	}
	n, err = ParseMemLimit("")
	if err != nil || n != 0 {
		t.Fatalf("%d %v", n, err)
	}
}
