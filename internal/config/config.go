package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server         ServerConfig              `yaml:"server"`
	Redis          RedisConfig               `yaml:"redis"`
	Cache          CacheConfig               `yaml:"cache"`
	Models         ModelsConfig              `yaml:"models"`
	Routing        RoutingConfig             `yaml:"routing"`
	CircuitBreaker CircuitBreakerConfig      `yaml:"circuit_breaker"`
	Providers      map[string]ProviderConfig `yaml:"providers"`
	Aliases        map[string][]AliasTarget  `yaml:"aliases"`
	Tenants        []TenantConfig            `yaml:"tenants"`
	Admin          AdminConfig               `yaml:"admin"`
	PricingFile    string                    `yaml:"pricing_file"`
}

type ServerConfig struct {
	Host             string        `yaml:"host"`
	Port             int           `yaml:"port"`
	AuthRequired     bool          `yaml:"auth_required"`
	ReadTimeout      time.Duration `yaml:"read_timeout"`
	WriteTimeout     time.Duration `yaml:"write_timeout"`
	IdleTimeout      time.Duration `yaml:"idle_timeout"`
	ShutdownTimeout  time.Duration `yaml:"shutdown_timeout"`
	MaxHeaderBytes   int           `yaml:"max_header_bytes"`
	MaxBodyBytes     int64         `yaml:"max_body_bytes"`
	LogPrompts       bool          `yaml:"log_prompts"`
	MetricsPublic    bool          `yaml:"metrics_public"`
	BehindProxy      bool          `yaml:"behind_proxy"`
	MemLimit         string        `yaml:"mem_limit"`
	RedisStrictReady bool          `yaml:"redis_strict_ready"`
}

type RedisConfig struct {
	URL          string `yaml:"url"`
	Enabled      bool   `yaml:"enabled"`
	PoolSize     int    `yaml:"pool_size"`
	MinIdleConns int    `yaml:"min_idle_conns"`
}

type CacheConfig struct {
	Enabled       bool            `yaml:"enabled"`
	SchemaVersion string          `yaml:"schema_version"`
	L1            L1Config        `yaml:"l1"`
	Exact         ExactConfig     `yaml:"exact"`
	Singleflight  SingleflightCfg `yaml:"singleflight"`
	Semantic      SemanticConfig  `yaml:"semantic"`
}

type L1Config struct {
	Enabled    bool          `yaml:"enabled"`
	MaxEntries int           `yaml:"max_entries"`
	TTL        time.Duration `yaml:"ttl"`
}

type ExactConfig struct {
	Enabled    bool          `yaml:"enabled"`
	DefaultTTL time.Duration `yaml:"default_ttl"`
}

type SingleflightCfg struct {
	Enabled     bool          `yaml:"enabled"`
	LockTTL     time.Duration `yaml:"lock_ttl"`
	WaitTimeout time.Duration `yaml:"wait_timeout"`
}

type SemanticConfig struct {
	Enabled          bool     `yaml:"enabled"`
	AllowedWorkloads []string `yaml:"allowed_workloads"`
}

type ModelsConfig struct {
	RefreshInterval time.Duration `yaml:"refresh_interval"`
}

type RoutingConfig struct {
	StickySessions bool          `yaml:"sticky_sessions"`
	StickyTTL      time.Duration `yaml:"sticky_ttl"`
	Weights        RouteWeights  `yaml:"weights"`
}

type RouteWeights struct {
	Cost          float64 `yaml:"cost"`
	Latency       float64 `yaml:"latency"`
	Error         float64 `yaml:"error"`
	Quota         float64 `yaml:"quota"`
	CacheAffinity float64 `yaml:"cache_affinity"`
}

type CircuitBreakerConfig struct {
	Failures    int           `yaml:"failures"`
	Cooldown    time.Duration `yaml:"cooldown"`
	HalfOpenMax int           `yaml:"half_open_max"`
}

type ProviderConfig struct {
	Enabled   bool          `yaml:"enabled"`
	Name      string        `yaml:"name"`
	BaseURL   string        `yaml:"base_url"`
	APIKeyEnv string        `yaml:"api_key_env"`
	Timeout   time.Duration `yaml:"timeout"`
	ZDR       bool          `yaml:"zdr"`
}

type AliasTarget struct {
	Provider string  `yaml:"provider"`
	Model    string  `yaml:"model"`
	Weight   float64 `yaml:"weight"`
}

type TenantConfig struct {
	ID                 string   `yaml:"id"`
	APIKey             string   `yaml:"api_key"`
	APIKeyEnv          string   `yaml:"api_key_env"`
	Admin              bool     `yaml:"admin"`
	RateLimitRPM       int      `yaml:"rate_limit_rpm"`
	MonthlyTokenBudget int64    `yaml:"monthly_token_budget"`
	AllowedProviders   []string `yaml:"allowed_providers"`
	AllowedModels      []string `yaml:"allowed_models"`
	CacheNamespace     string   `yaml:"cache_namespace"`
	SharedCache        bool     `yaml:"shared_cache"`
}

type AdminConfig struct {
	APIKeyEnv string `yaml:"api_key_env"`
}

func Load() (*Config, error) {
	path := firstExisting(
		os.Getenv("CONFIG_PATH"),
		"config.yaml",
		"config.example.yaml",
	)
	if path == "" {
		return nil, fmt.Errorf("no config file found (set CONFIG_PATH or add config.yaml)")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}
	cfg := Defaults()
	if err := yaml.Unmarshal(raw, cfg); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", path, err)
	}
	cfg.applyEnv()
	cfg.normalize()
	return cfg, nil
}

func Defaults() *Config {
	return &Config{
		Server: ServerConfig{
			Host:             "0.0.0.0",
			Port:             8080,
			AuthRequired:     true,
			ReadTimeout:      15 * time.Minute,
			WriteTimeout:     15 * time.Minute,
			IdleTimeout:      90 * time.Second,
			ShutdownTimeout:  2 * time.Minute,
			MaxHeaderBytes:   16 << 10,
			MaxBodyBytes:     16 << 20,
			MetricsPublic:    false,
			RedisStrictReady: true,
		},
		Redis: RedisConfig{URL: "redis://localhost:6379/0", Enabled: true, PoolSize: 16, MinIdleConns: 2},
		Cache: CacheConfig{
			Enabled:       true,
			SchemaVersion: "v1",
			L1:            L1Config{Enabled: true, MaxEntries: 10000, TTL: time.Minute},
			Exact:         ExactConfig{Enabled: true, DefaultTTL: time.Hour},
			Singleflight:  SingleflightCfg{Enabled: true, LockTTL: 5 * time.Minute, WaitTimeout: 4 * time.Minute},
			Semantic:      SemanticConfig{Enabled: false},
		},
		Models: ModelsConfig{RefreshInterval: 5 * time.Minute},
		Routing: RoutingConfig{
			StickySessions: true,
			StickyTTL:      time.Hour,
			Weights:        RouteWeights{Cost: 0.35, Latency: 0.25, Error: 0.20, Quota: 0.10, CacheAffinity: 0.10},
		},
		CircuitBreaker: CircuitBreakerConfig{Failures: 5, Cooldown: 30 * time.Second, HalfOpenMax: 1},
		Providers:      map[string]ProviderConfig{},
		Aliases:        map[string][]AliasTarget{},
		PricingFile:    "data/pricing.yaml",
	}
}

func (c *Config) applyEnv() {
	if v := os.Getenv("GATEWAY_HOST"); v != "" {
		c.Server.Host = v
	}
	if v := os.Getenv("GATEWAY_PORT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			c.Server.Port = n
		}
	}
	if v := os.Getenv("REDIS_URL"); v != "" {
		c.Redis.URL = v
	}
	if v := os.Getenv("CACHE_SCHEMA_VERSION"); v != "" {
		c.Cache.SchemaVersion = v
	}
	if v := os.Getenv("GOMEMLIMIT"); v != "" && c.Server.MemLimit == "" {
		c.Server.MemLimit = v
	}
}

func (c *Config) normalize() {
	if c.Cache.SchemaVersion == "" {
		c.Cache.SchemaVersion = "v1"
	}
	if c.Cache.L1.MaxEntries <= 0 {
		c.Cache.L1.MaxEntries = 10000
	}
	if c.CircuitBreaker.Failures <= 0 {
		c.CircuitBreaker.Failures = 5
	}
	if c.CircuitBreaker.HalfOpenMax <= 0 {
		c.CircuitBreaker.HalfOpenMax = 1
	}
	if c.Server.ShutdownTimeout <= 0 {
		c.Server.ShutdownTimeout = 2 * time.Minute
	}
	if c.Server.MaxHeaderBytes <= 0 {
		c.Server.MaxHeaderBytes = 16 << 10
	}
	if c.Server.MaxBodyBytes <= 0 {
		c.Server.MaxBodyBytes = 16 << 20
	}
	if c.Redis.PoolSize <= 0 {
		c.Redis.PoolSize = 16
	}
	if c.Redis.MinIdleConns < 0 {
		c.Redis.MinIdleConns = 0
	}
	for i := range c.Tenants {
		if c.Tenants[i].CacheNamespace == "" {
			c.Tenants[i].CacheNamespace = c.Tenants[i].ID
		}
		if c.Tenants[i].APIKey == "" && c.Tenants[i].APIKeyEnv != "" {
			c.Tenants[i].APIKey = os.Getenv(c.Tenants[i].APIKeyEnv)
		}
	}
}

func (c *Config) Addr() string {
	return fmt.Sprintf("%s:%d", c.Server.Host, c.Server.Port)
}

func (c *Config) ProviderAPIKey(p ProviderConfig) string {
	if p.APIKeyEnv == "" {
		return ""
	}
	if v := os.Getenv(p.APIKeyEnv); v != "" {
		return v
	}
	// OpenCode documents both OPENCODE_GO_API_KEY and OPENCODE_API_KEY.
	if p.APIKeyEnv == "OPENCODE_GO_API_KEY" {
		return os.Getenv("OPENCODE_API_KEY")
	}
	return ""
}

func (c *Config) AdminKey() string {
	if c.Admin.APIKeyEnv != "" {
		if v := os.Getenv(c.Admin.APIKeyEnv); v != "" {
			return v
		}
	}
	for _, t := range c.Tenants {
		if t.Admin && t.APIKey != "" {
			return t.APIKey
		}
	}
	return os.Getenv("GATEWAY_API_KEY")
}

// ParseMemLimit accepts values like 384MiB, 512MB, 1GiB, 400000000.
func ParseMemLimit(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, nil
	}
	upper := strings.ToUpper(s)
	mult := int64(1)
	switch {
	case strings.HasSuffix(upper, "GIB") || strings.HasSuffix(upper, "GB"):
		mult = 1 << 30
		upper = strings.TrimSuffix(strings.TrimSuffix(upper, "GIB"), "GB")
	case strings.HasSuffix(upper, "MIB") || strings.HasSuffix(upper, "MB"):
		mult = 1 << 20
		upper = strings.TrimSuffix(strings.TrimSuffix(upper, "MIB"), "MB")
	case strings.HasSuffix(upper, "KIB") || strings.HasSuffix(upper, "KB"):
		mult = 1 << 10
		upper = strings.TrimSuffix(strings.TrimSuffix(upper, "KIB"), "KB")
	case strings.HasSuffix(upper, "B"):
		upper = strings.TrimSuffix(upper, "B")
	}
	n, err := strconv.ParseFloat(strings.TrimSpace(upper), 64)
	if err != nil {
		return 0, fmt.Errorf("mem_limit %q: %w", s, err)
	}
	return int64(n * float64(mult)), nil
}

func firstExisting(paths ...string) string {
	for _, p := range paths {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}
