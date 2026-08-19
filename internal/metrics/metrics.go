package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

type Metrics struct {
	RequestsTotal           *prometheus.CounterVec
	CacheL1Hits             prometheus.Counter
	CacheL2Hits             prometheus.Counter
	CacheMisses             prometheus.Counter
	CacheHitRatio           prometheus.Gauge
	SingleflightSaved       prometheus.Counter
	UpstreamRequests        *prometheus.CounterVec
	UpstreamLatency         *prometheus.HistogramVec
	FirstTokenLatency       *prometheus.HistogramVec
	TokensInput             *prometheus.CounterVec
	TokensOutput            *prometheus.CounterVec
	CachedTokens            prometheus.Counter
	EstimatedCost           *prometheus.CounterVec
	EstimatedCostSaved      prometheus.Counter
	PrefixReuse             prometheus.Histogram
	CredentialActive        *prometheus.GaugeVec
}

func New(reg prometheus.Registerer) *Metrics {
	if reg == nil {
		reg = prometheus.DefaultRegisterer
	}
	f := promauto.With(reg)
	return &Metrics{
		RequestsTotal: f.NewCounterVec(prometheus.CounterOpts{
			Name: "llmgw_requests_total",
			Help: "Gateway requests by protocol, cache status, and HTTP code class.",
		}, []string{"protocol", "cache", "code"}),
		CacheL1Hits: f.NewCounter(prometheus.CounterOpts{Name: "llmgw_cache_l1_hits_total", Help: "L1 hits"}),
		CacheL2Hits: f.NewCounter(prometheus.CounterOpts{Name: "llmgw_cache_l2_hits_total", Help: "L2 hits"}),
		CacheMisses: f.NewCounter(prometheus.CounterOpts{Name: "llmgw_cache_misses_total", Help: "Cache misses"}),
		CacheHitRatio: f.NewGauge(prometheus.GaugeOpts{Name: "llmgw_cache_hit_ratio", Help: "Hit ratio"}),
		SingleflightSaved: f.NewCounter(prometheus.CounterOpts{
			Name: "llmgw_singleflight_saved_requests_total",
			Help: "Requests that reused an in-flight identical generation",
		}),
		UpstreamRequests: f.NewCounterVec(prometheus.CounterOpts{
			Name: "llmgw_upstream_requests_total",
			Help: "Upstream calls",
		}, []string{"provider", "credential_id", "protocol", "code"}),
		UpstreamLatency: f.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "llmgw_upstream_latency_seconds",
			Help:    "Upstream latency",
			Buckets: prometheus.DefBuckets,
		}, []string{"provider", "credential_id"}),
		FirstTokenLatency: f.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "llmgw_first_token_latency_seconds",
			Help:    "Time to first streamed token",
			Buckets: []float64{0.01, 0.05, 0.1, 0.25, 0.5, 1, 2, 5, 10},
		}, []string{"provider", "credential_id", "cache"}),
		TokensInput: f.NewCounterVec(prometheus.CounterOpts{
			Name: "llmgw_tokens_input_total", Help: "Input tokens",
		}, []string{"provider", "credential_id"}),
		TokensOutput: f.NewCounterVec(prometheus.CounterOpts{
			Name: "llmgw_tokens_output_total", Help: "Output tokens",
		}, []string{"provider", "credential_id"}),
		CachedTokens: f.NewCounter(prometheus.CounterOpts{
			Name: "llmgw_cached_tokens_total", Help: "Tokens served from gateway cache",
		}),
		EstimatedCost: f.NewCounterVec(prometheus.CounterOpts{
			Name: "llmgw_estimated_cost_total", Help: "Estimated USD spent",
		}, []string{"provider", "credential_id"}),
		EstimatedCostSaved: f.NewCounter(prometheus.CounterOpts{
			Name: "llmgw_estimated_cost_saved_total", Help: "Estimated USD saved by gateway cache",
		}),
		PrefixReuse: f.NewHistogram(prometheus.HistogramOpts{
			Name:    "llmgw_prefix_reuse_ratio",
			Help:    "Session prefix reuse (1 = identical prefix)",
			Buckets: []float64{0, 0.25, 0.5, 0.75, 1},
		}),
		CredentialActive: f.NewGaugeVec(prometheus.GaugeOpts{
			Name: "llmgw_credential_active_requests",
			Help: "In-flight upstream requests per credential",
		}, []string{"provider", "credential_id"}),
	}
}

func (m *Metrics) ObserveHitRatio(hits, total float64) {
	if total <= 0 {
		m.CacheHitRatio.Set(0)
		return
	}
	m.CacheHitRatio.Set(hits / total)
}
