package cost

import (
	"os"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
	"llmgw/internal/protocol"
)

type Rates struct {
	Input      float64 `yaml:"input"`
	Output     float64 `yaml:"output"`
	CachedRead float64 `yaml:"cached_read"`
	CachedWrite float64 `yaml:"cached_write"`
}

type File struct {
	Currency string           `yaml:"currency"`
	Models   map[string]Rates `yaml:"models"`
}

type Table struct {
	mu     sync.RWMutex
	models map[string]Rates
}

func Load(path string) *Table {
	t := &Table{models: map[string]Rates{}}
	if path == "" {
		return t
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return t
	}
	var f File
	if err := yaml.Unmarshal(b, &f); err != nil {
		return t
	}
	if f.Models != nil {
		t.models = f.Models
	}
	return t
}

func (t *Table) Estimate(provider, model string, u protocol.Usage) (actual, savedByGW, savedByPromptCache float64) {
	r := t.lookup(provider, model)
	in := float64(u.InputTokens-u.CachedInputTokens) / 1e6 * r.Input
	if in < 0 {
		in = 0
	}
	out := float64(u.OutputTokens) / 1e6 * r.Output
	cached := float64(u.CachedInputTokens) / 1e6 * r.CachedRead
	write := float64(u.CacheWriteTokens) / 1e6 * r.CachedWrite
	actual = in + out + cached + write
	// if this usage was served from gateway cache, the whole actual is "saved"
	savedByPromptCache = float64(u.CachedInputTokens)/1e6*(r.Input-r.CachedRead)
	if savedByPromptCache < 0 {
		savedByPromptCache = 0
	}
	savedByGW = float64(u.InputTokens)/1e6*r.Input + float64(u.OutputTokens)/1e6*r.Output
	return actual, savedByGW, savedByPromptCache
}

func (t *Table) lookup(provider, model string) Rates {
	t.mu.RLock()
	defer t.mu.RUnlock()
	keys := []string{
		provider + "/" + model,
		strings.ReplaceAll(provider, "_", "-") + "/" + model,
		model,
		"default",
	}
	for _, k := range keys {
		if r, ok := t.models[k]; ok {
			return r
		}
	}
	return Rates{Input: 1, Output: 3, CachedRead: 0.1}
}
