package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"llmgw/internal/auth"
	"llmgw/internal/cache"
	"llmgw/internal/canonical"
	"llmgw/internal/config"
	"llmgw/internal/cost"
	"llmgw/internal/metrics"
	"llmgw/internal/protocol"
	"llmgw/internal/provider"
	"llmgw/internal/router"
	"llmgw/internal/singleflight"
	"llmgw/internal/usage"
)

type Gateway struct {
	Cfg    *config.Config
	Log    *slog.Logger
	Auth   *auth.Service
	Cache  *cache.Store
	SF     *singleflight.Group
	Reg    *provider.Registry
	Router *router.Router
	Usage  *usage.Store
	Cost   *cost.Table
	M      *metrics.Metrics
	Ready  func() error
	Admin  string
}

func (g *Gateway) handleModels(w http.ResponseWriter, r *http.Request) {
	t, code, msg := g.Auth.Authenticate(r)
	if code != 0 {
		writeError(w, protocol.ChatCompletions, code, openaiErrorType(code), msg)
		return
	}
	ms := g.Reg.Models()
	type item struct {
		ID      string `json:"id"`
		Object  string `json:"object"`
		Created int64  `json:"created"`
		OwnedBy string `json:"owned_by"`
	}
	var data []item
	seen := map[string]bool{}
	for _, m := range ms {
		if !g.Auth.AllowModel(t, m.Provider, m.UpstreamID) {
			continue
		}
		id := provider.PublicProviderID(m.Provider) + "/" + m.UpstreamID
		if seen[id] {
			continue
		}
		seen[id] = true
		data = append(data, item{ID: id, Object: "model", Created: time.Now().Unix(), OwnedBy: m.Provider})
	}
	for alias := range g.Cfg.Aliases {
		if !seen[alias] {
			data = append(data, item{ID: alias, Object: "model", Created: time.Now().Unix(), OwnedBy: "alias"})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"object": "list", "data": data})
}

func (g *Gateway) handleChat(w http.ResponseWriter, r *http.Request) {
	g.handleProtocol(w, r, protocol.ChatCompletions)
}

func (g *Gateway) handleResponses(w http.ResponseWriter, r *http.Request) {
	g.handleProtocol(w, r, protocol.Responses)
}

func (g *Gateway) handleMessages(w http.ResponseWriter, r *http.Request) {
	g.handleProtocol(w, r, protocol.Messages)
}

func (g *Gateway) handleProtocol(w http.ResponseWriter, r *http.Request, proto protocol.Protocol) {
	start := time.Now()
	reqID := r.Header.Get("x-request-id")
	if reqID == "" {
		reqID = "req_" + nonce(8)
	}
	tenant, code, msg := g.Auth.Authenticate(r)
	if code != 0 {
		writeError(w, proto, code, openaiErrorType(code), msg)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 32<<20))
	if err != nil {
		writeError(w, proto, 400, "invalid_request_error", "read body")
		return
	}
	parsed, err := protocol.Parse(proto, body)
	if err != nil {
		writeError(w, proto, 400, "invalid_request_error", err.Error())
		return
	}
	cs, err := g.Router.Resolve(parsed.Model)
	if err != nil {
		writeError(w, proto, 400, "invalid_request_error", err.Error())
		return
	}
	var allowed []router.Candidate
	for _, c := range cs {
		if g.Auth.AllowModel(tenant, c.Provider, c.Model) {
			allowed = append(allowed, c)
		}
	}
	if len(allowed) == 0 {
		writeError(w, proto, 403, "permission_error", "model not allowed for tenant")
		return
	}
	session := r.Header.Get("x-session-id")
	if session == "" {
		session = r.Header.Get("x-conversation-id")
	}
	ordered := g.Router.Pick(r.Context(), tenant.ID, session, allowed)
	if len(ordered) == 0 {
		writeError(w, proto, 503, "server_error", "no available provider")
		return
	}
	primary := ordered[0]
	mode := cache.ParseMode(r.Header.Get("x-cache-mode"))
	if strings.EqualFold(r.Header.Get("cache-control"), "no-cache") {
		mode = cache.ModeBypass
	}
	ns := tenant.CacheNamespace
	if tenant.SharedCache {
		ns = "shared"
	}
	canon := parsed.ToCanonical(ns, primary.Provider, primary.Model)
	cacheKey, hash, err := canon.CacheKey(g.Cfg.Cache.SchemaVersion)
	if err != nil {
		writeError(w, proto, 400, "invalid_request_error", "canonicalize: "+err.Error())
		return
	}
	prefix := canonical.Prefix(parsed.Messages, parsed.Tools, parsed.System)
	reuse, _ := g.Router.TrackPrefix(r.Context(), tenant.ID, session, prefix.Hash, prefix.Length)
	g.M.PrefixReuse.Observe(reuse)

	eligible := cache.Eligible(mode, parsed)
	var hitKind cache.HitKind = cache.HitNone
	if mode == cache.ModeBypass {
		hitKind = cache.HitBypass
	}

	if eligible && mode != cache.ModeBypass {
		if e, kind, err := g.Cache.Get(r.Context(), cacheKey); err == nil && e != nil {
			g.serveCached(w, r, proto, parsed, e, kind, cacheKey, reqID, start, tenant, prefix, reuse)
			return
		}
	}

	type outcome struct {
		comp       protocol.Completion
		prov       string
		model      string
		cred       string
		status     int
		latency    time.Duration
		firstTok   time.Duration
		err        error
		streamedTo http.ResponseWriter
	}

	doUpstream := func(streamTo http.ResponseWriter) (outcome, error) {
		var last outcome
		for _, c := range ordered {
			if last.streamedTo != nil {
				break
			}
			nat := c.ProviderImpl.NativeProtocol(c.Model)
			fwd, err := protocol.TranslateRequest(parsed.Raw, proto, nat, c.Model)
			if err != nil {
				last = outcome{err: err, status: 400, prov: c.Provider, model: c.Model}
				continue
			}
			credIDs := credentialIDs(c.ProviderImpl)
			orderedCreds := g.Router.OrderCredentials(r.Context(), tenant.ID, session, c.Provider, credIDs)
			for _, credID := range orderedCreds {
				var first time.Duration
				var dest http.ResponseWriter
				on := func(protocol.StreamEvent) error { return nil }
				if parsed.Stream && streamTo != nil {
					flusher, _ := streamTo.(http.Flusher)
					headersWritten := false
					on = func(ev protocol.StreamEvent) error {
						if ev.Err != nil {
							return ev.Err
						}
						meaningful := ev.DeltaContent != "" || ev.DeltaReasoning != "" || len(ev.ToolCalls) > 0
						if !headersWritten && meaningful {
							dest = streamTo
							first = time.Since(start)
							setCommonHeaders(streamTo, c.Provider, displayModel(parsed.Model, c), cache.HitNone, cacheKey, "", start, first, reqID, credID)
							streamTo.Header().Set("Content-Type", "text/event-stream")
							streamTo.Header().Set("Cache-Control", "no-cache")
							streamTo.Header().Set("x-prefix-hash", prefix.Hash)
							streamTo.Header().Set("x-prefix-length", strconv.Itoa(prefix.Length))
							streamTo.Header().Set("x-prefix-reuse-ratio", strconv.FormatFloat(reuse, 'f', 3, 64))
							streamTo.WriteHeader(http.StatusOK)
							headersWritten = true
						}
						if !headersWritten || proto != nat || len(ev.Raw) == 0 {
							return nil
						}
						if ev.Event != "" {
							if _, err := streamTo.Write([]byte("event: " + ev.Event + "\n")); err != nil {
								return err
							}
						}
						if _, err := streamTo.Write(append(append([]byte("data: "), ev.Raw...), '\n', '\n')); err != nil {
							return err
						}
						if flusher != nil {
							flusher.Flush()
						}
						return nil
					}
				}
				g.M.CredentialActive.WithLabelValues(c.Provider, credID).Inc()
				res, err := c.ProviderImpl.Do(r.Context(), nat, c.Model, fwd, parsed.Stream, on, credID)
				g.M.CredentialActive.WithLabelValues(c.Provider, credID).Dec()
				used := res.CredentialID
				if used == "" {
					used = credID
				}
				g.Router.ReportCred(c.Provider, used, res.Status, err)
				g.M.UpstreamRequests.WithLabelValues(c.Provider, used, string(nat), strconv.Itoa(res.Status)).Inc()
				g.M.UpstreamLatency.WithLabelValues(c.Provider, used).Observe(res.Latency.Seconds())
				last = outcome{
					comp: res.Completion, prov: c.Provider, model: c.Model, cred: used,
					status: res.Status, latency: res.Latency, firstTok: first, err: err, streamedTo: dest,
				}
				if err == nil && res.Status < 400 {
					return last, nil
				}
				if dest != nil {
					return last, err
				}
				if !retryable(res.Status, err) {
					break
				}
			}
			g.Router.Report(c.Provider, last.status, last.err)
		}
		if last.err == nil {
			last.err = errors.New("all providers failed")
		}
		return last, last.err
	}

	run := func() (any, error) {
		if eligible {
			if e, ok := g.Cache.Peek(r.Context(), cacheKey); ok {
				return outcome{comp: e.Completion, prov: e.Provider, model: e.Model, status: 200}, nil
			}
		}
		out, err := doUpstream(w)
		if err == nil && eligible && out.status < 400 {
			_ = g.Cache.Set(r.Context(), cacheKey, cache.Entry{
				Completion: out.comp, Provider: out.prov, Model: out.model, Bytes: len(body),
			})
		}
		return out, err
	}

	var out outcome
	if g.Cfg.Cache.Singleflight.Enabled {
		v, err, shared := g.SF.Do(r.Context(), hash, run)
		if shared {
			g.Cache.Stats.SingleflightSaved.Add(1)
			g.M.SingleflightSaved.Inc()
		}
		if err != nil && singleflight.IsWaiterReleased(err) {
			if e, kind, gerr := g.Cache.Get(r.Context(), cacheKey); gerr == nil && e != nil {
				g.serveCached(w, r, proto, parsed, e, kind, cacheKey, reqID, start, tenant, prefix, reuse)
				return
			}
			v, err, _ = g.SF.Do(r.Context(), hash+"-retry", run)
		}
		if err != nil {
			st := 502
			if out2, ok := v.(outcome); ok && out2.status > 0 {
				st = out2.status
			}
			g.M.RequestsTotal.WithLabelValues(string(proto), string(hitKind), strconv.Itoa(st)).Inc()
			writeError(w, proto, st, openaiErrorType(st), err.Error())
			return
		}
		out = v.(outcome)
	} else {
		var err error
		out, err = doUpstream(w)
		if err != nil {
			st := out.status
			if st == 0 {
				st = 502
			}
			g.M.RequestsTotal.WithLabelValues(string(proto), string(hitKind), strconv.Itoa(st)).Inc()
			writeError(w, proto, st, openaiErrorType(st), err.Error())
			return
		}
		if eligible && out.status < 400 {
			_ = g.Cache.Set(r.Context(), cacheKey, cache.Entry{
				Completion: out.comp, Provider: out.prov, Model: out.model, Bytes: len(body),
			})
		}
	}

	if out.comp.Created == 0 {
		out.comp.Created = time.Now().Unix()
	}
	if out.comp.ID == "" {
		out.comp.ID = "chatcmpl-" + nonce(10)
	}
	actual, savedGW, _ := g.Cost.Estimate(out.prov, out.model, out.comp.Usage)
	cred := out.cred
	if cred == "" {
		cred = "default"
	}
	g.Usage.AddCred(r.Context(), tenant.ID, out.prov, cred, out.comp.Usage, actual, out.latency, out.status, false)
	g.M.TokensInput.WithLabelValues(out.prov, cred).Add(float64(out.comp.Usage.InputTokens))
	g.M.TokensOutput.WithLabelValues(out.prov, cred).Add(float64(out.comp.Usage.OutputTokens))
	g.M.EstimatedCost.WithLabelValues(out.prov, cred).Add(actual)
	_ = savedGW
	g.Router.RememberStickyCred(r.Context(), tenant.ID, session, out.prov, out.model, cred)
	g.M.RequestsTotal.WithLabelValues(string(proto), "MISS", "200").Inc()
	if g.M != nil {
		g.M.ObserveHitRatio(float64(g.Cache.Stats.L1Hits.Load()+g.Cache.Stats.L2Hits.Load()), float64(g.Cache.Stats.L1Hits.Load()+g.Cache.Stats.L2Hits.Load()+g.Cache.Stats.Misses.Load()))
	}

	if parsed.Stream {
		if out.streamedTo == w {
			return
		}
		setCommonHeaders(w, out.prov, parsed.Model, cache.HitNone, cacheKey, "", start, out.firstTok, reqID, out.cred)
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		flush := func() {
			if flusher != nil {
				flusher.Flush()
			}
		}
		_ = protocol.WriteStream(w, proto, out.comp, parsed.Model, flush)
		return
	}

	setCommonHeaders(w, out.prov, parsed.Model, cache.HitNone, cacheKey, "", start, 0, reqID, out.cred)
	w.Header().Set("x-prefix-hash", prefix.Hash)
	w.Header().Set("x-prefix-length", strconv.Itoa(prefix.Length))
	w.Header().Set("x-prefix-reuse-ratio", strconv.FormatFloat(reuse, 'f', 3, 64))
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(protocol.EncodeCompletion(proto, out.comp, parsed.Model))

	g.Log.Info("request",
		"request_id", reqID,
		"tenant", tenant.ID,
		"provider", out.prov,
		"credential_id", out.cred,
		"model", out.model,
		"cache", "MISS",
		"input_tokens", out.comp.Usage.InputTokens,
		"cached_tokens", out.comp.Usage.CachedInputTokens,
		"output_tokens", out.comp.Usage.OutputTokens,
		"latency_ms", time.Since(start).Milliseconds(),
		"prefix_hash", prefix.Hash,
		"prefix_length", prefix.Length,
		"prefix_reuse_ratio", reuse,
	)
}

func (g *Gateway) serveCached(w http.ResponseWriter, r *http.Request, proto protocol.Protocol, parsed *protocol.ParsedRequest, e *cache.Entry, kind cache.HitKind, cacheKey, reqID string, start time.Time, tenant *auth.Tenant, prefix canonical.PrefixInfo, reuse float64) {
	g.M.RequestsTotal.WithLabelValues(string(proto), string(kind), "200").Inc()
	g.M.CachedTokens.Add(float64(e.Completion.Usage.InputTokens + e.Completion.Usage.OutputTokens))
	_, saved, _ := g.Cost.Estimate(e.Provider, e.Model, e.Completion.Usage)
	g.Cache.AddCostSaved(saved)
	g.M.EstimatedCostSaved.Add(saved)
	g.Usage.Add(r.Context(), tenant.ID, e.Provider, e.Completion.Usage, 0, time.Since(start), 200, true)
	g.M.ObserveHitRatio(float64(g.Cache.Stats.L1Hits.Load()+g.Cache.Stats.L2Hits.Load()), float64(g.Cache.Stats.L1Hits.Load()+g.Cache.Stats.L2Hits.Load()+g.Cache.Stats.Misses.Load()))
	setCommonHeaders(w, e.Provider, parsed.Model, kind, cacheKey, cache.Age(e), start, 0, reqID, "")
	w.Header().Set("x-prefix-hash", prefix.Hash)
	w.Header().Set("x-prefix-length", strconv.Itoa(prefix.Length))
	w.Header().Set("x-prefix-reuse-ratio", strconv.FormatFloat(reuse, 'f', 3, 64))
	if parsed.Stream {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		flusher, _ := w.(http.Flusher)
		flush := func() {
			if flusher != nil {
				flusher.Flush()
			}
		}
		_ = protocol.WriteStream(w, proto, e.Completion, parsed.Model, flush)
	} else {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(protocol.EncodeCompletion(proto, e.Completion, parsed.Model))
	}
	g.Log.Info("request",
		"request_id", reqID,
		"tenant", tenant.ID,
		"provider", e.Provider,
		"model", e.Model,
		"cache", string(kind),
		"input_tokens", e.Completion.Usage.InputTokens,
		"output_tokens", e.Completion.Usage.OutputTokens,
		"latency_ms", time.Since(start).Milliseconds(),
	)
}

func retryable(status int, err error) bool {
	if status == 429 || status >= 500 {
		return true
	}
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "timeout") || strings.Contains(msg, "connection")
}

func displayModel(requested string, c router.Candidate) string {
	if requested != "" {
		return requested
	}
	return provider.PublicProviderID(c.Provider) + "/" + c.Model
}

func credentialIDs(p provider.Provider) []string {
	infos := p.ListCredentials()
	if len(infos) == 0 {
		return []string{"default"}
	}
	out := make([]string, 0, len(infos))
	for _, i := range infos {
		out = append(out, i.ID)
	}
	return out
}

func setCommonHeaders(w http.ResponseWriter, prov, model string, cacheKind cache.HitKind, key, age string, start time.Time, first time.Duration, reqID, cred string) {
	w.Header().Set("x-gateway-provider", provider.PublicProviderID(prov))
	w.Header().Set("x-gateway-model", model)
	if cred != "" {
		w.Header().Set("x-gateway-credential", cred)
	}
	w.Header().Set("x-cache", string(cacheKind))
	if key != "" {
		w.Header().Set("x-cache-key", key)
	}
	if age != "" {
		w.Header().Set("x-cache-age", age)
	}
	w.Header().Set("x-upstream-latency-ms", strconv.FormatInt(time.Since(start).Milliseconds(), 10))
	if first > 0 {
		w.Header().Set("x-first-token-ms", strconv.FormatInt(first.Milliseconds(), 10))
	}
	w.Header().Set("x-request-id", reqID)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func nonce(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
