# LLM API Gateway — Architecture

Verified against official public docs on 2026-08-19.

## Upstream providers

### OpenCode Go

| Item | Value |
| --- | --- |
| Base URL | `https://opencode.ai/zen/go/v1` |
| Auth | `Authorization: Bearer $OPENCODE_GO_API_KEY` |
| Models | `GET /v1/models` (public, OpenAI list shape) |
| Chat Completions | `POST /v1/chat/completions` |
| Responses | `POST /v1/responses` (`grok-4.5`, `gpt-5.6-luna`) |
| Messages | `POST /v1/messages` (MiniMax, Qwen) |
| Streaming | Yes (native per protocol) |
| Tools | Yes (per models.dev / Zen docs) |
| Usage | Standard usage objects + prompt-cache token fields when upstream returns them |
| Limits | 5h $12 / week $30 / month $60 (account-side; gateway does not bypass) |

Native protocol is **per model**, not one protocol for the whole provider. The adapter picks the documented endpoint and never invents capabilities.

### Command Code Provider API

| Item | Value |
| --- | --- |
| Base URL | `https://api.commandcode.ai/provider/v1` |
| Auth | `Authorization: Bearer $COMMANDCODE_API_KEY` or `x-api-key` |
| Models | `GET /provider/v1/models` (public) |
| Chat Completions | `POST /provider/v1/chat/completions` |
| Messages | `POST /provider/v1/messages` |
| Responses | Not documented — gateway translates to Chat Completions |
| Streaming | `stream: true`; usage on final chunk / `message_delta` |
| Claude models | Must use `/messages` (400 if sent to chat) |
| Extra | Optional `x-cmdc-zdr: 1` (never enabled automatically) |

## Request path

```
client
  -> auth / tenant
  -> protocol parse (chat | responses | messages)
  -> model resolve (provider/model or alias)
  -> cache policy
  -> L1 exact -> L2 Redis exact
  -> distributed singleflight
  -> sticky + scored routing + circuit breaker
  -> protocol translate (only if needed)
  -> provider (byte-stable model swap on native path)
  -> usage / cost
  -> cache commit (complete success only)
  -> synthesize client protocol (incl. stream replay)
```

## Cache

- **L1**: process-local LRU + TTL.
- **L2**: Redis exact body, key `llmgw:{schema}:{tenant}:resp:{sha256}`.
- **Canonicalization**: structural JSON only (sorted object keys). Message strings, indentation, tool order, and schemas are not rewritten.
- **Stream flag is not part of the key** so stream and non-stream share one stored completion.
- **Auto**: cache only deterministic-looking successful completions (temperature 0 / seed / explicit force). Never cache `previous_response_id`, incomplete streams, or errors.
- **Semantic**: optional, default off, tenant-isolated, refused for tool loops / mutations.
- **Singleflight**: local group + Redis `SET NX` lock. Waiters poll the committed cache entry.

## Routing

Priority: exact `provider/model` → sticky session → scored alias candidates (cost, latency, errors, quota, cache affinity) → circuit-open skip → failover **only before the first client byte**.

## Prompt-cache preservation

On a native-protocol hop the gateway replaces only the `model` field in the original JSON (`sjson`) and forwards the rest unchanged. No timestamps, request IDs, or reordered tools are injected into the upstream body.

## Storage

Redis for cache, locks, sticky sessions, short counters, circuit snapshot. In-process fallback when Redis is absent (dev). Optional SQLite for durable usage (off unless configured).
