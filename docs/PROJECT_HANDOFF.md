# Project handoff — llm-api-gateway

Canonical long-form handoff. Update this file in the same commit as runtime, schema, safety, or policy changes. Short working state lives in `.agent/`.

Repo: https://github.com/huhumeme2002/llm-api-gateway  
Module: `llmgw`  
Last code commit at time of writing: `9b05f61` (per-credential API keys + dedicated proxies)

## What this is

A self-hosted gateway so coding agents and SDKs talk to:

```text
http://127.0.0.1:8080/v1/models
http://127.0.0.1:8080/v1/chat/completions
http://127.0.0.1:8080/v1/responses
http://127.0.0.1:8080/v1/messages
```

instead of caring which upstream is used.

Verified public APIs (2026-08-19):

| Provider | Base | Auth | Native surfaces |
| --- | --- | --- | --- |
| OpenCode Go | `https://opencode.ai/zen/go/v1` | Bearer | Chat for most models; `/responses` for `grok-4.5` and `gpt-5.6-luna`; `/messages` for MiniMax and Qwen |
| Command Code | `https://api.commandcode.ai/provider/v1` | Bearer or `x-api-key` | Chat for non-Claude; `/messages` **required** for `claude*`; no Responses API |

Do not invent endpoints. Refresh catalogs from `GET .../models`.

## Runtime

- Go 1.23+, Redis 7 for L2 / locks / sticky (L1-only if Redis is down and `redis_strict_ready` allows it).
- Start: `cp .env.example .env`, fill keys, `docker compose up -d --build` or `make run`.
- Compose binds **127.0.0.1:8080**; Redis is internal-only. Put Caddy in front (`deploy/Caddyfile`).
- VPS knobs: `config.vps.yaml`, `GOMEMLIMIT=384MiB`, Redis `maxmemory 128mb allkeys-lru`.
- systemd: `deploy/install.sh` + `deploy/llmgw.service`.
- Gateway clients send `Authorization: Bearer $GATEWAY_API_KEY` (or `x-api-key`).

Env names (never commit values): `OPENCODE_GO_API_KEY` / `OPENCODE_API_KEY`, `COMMANDCODE_API_KEY`, `GATEWAY_API_KEY`, `GATEWAY_ADMIN_API_KEY`, `REDIS_URL`. Multi-key: `OPENCODE_GO_KEY_01` + `OPENCODE_GO_PROXY_01`, etc.

## Schema / public headers

Cache key: `llmgw:{schema}:{tenant}:resp:{sha256}` of canonical request (tenant, protocol, provider, model, messages, tools, sampling, etc.). **Not** stream flag, **not** API keys, **not** proxy URLs.

Sticky Redis value: `provider|model` or `provider|model|credential_id`.

Response headers (no secrets): `x-gateway-provider`, `x-gateway-model`, `x-gateway-credential`, `x-cache` (`HIT-L1` / `HIT-REDIS` / `MISS` / `BYPASS`), `x-cache-key`, `x-request-id`, prefix diagnostics.

Admin: `/health`, `/ready`, `/metrics` (admin key unless `metrics_public`), `/admin/cache/stats`, `/admin/providers`, `/admin/models`, `/admin/usage`, **`GET /admin/credentials`** (ids + health + counters only).

## Credentials and proxies (policy)

- `providers.<name>.credentials[]`: `id`, `api_key_env`, optional `proxy_env`.
- Legacy `api_key_env` → one credential `default`.
- Each credential: its own `http.Client` / `http.Transport`. HTTP, HTTPS, SOCKS5. Empty proxy = direct.
- `proxy.direct_fallback` default **false**: a dead proxy fails that credential; do not retry it without the proxy.
- `x-session-id` keeps the same credential while the circuit is closed.
- Failover: next healthy credential, then next provider. Never after the first client stream byte.
- Singleflight winner picks the credential **once** so identical in-flight requests do not fan out across keys.

## Safety

- Never log raw prompts unless `server.log_prompts`.
- Redact bearer tokens, `sk-…`, and `user:pass@` in errors and admin messages.
- Do not log or return proxy URLs or API keys from `/admin/*` or metrics labels (credential **id** only).
- Do not bypass provider rate limits.
- Native-protocol forward: change only `model` so prompt prefixes stay stable.

## Tests

```bash
go test ./...
go test -race ./...   # Windows: CGO_ENABLED=1 and gcc on PATH
```

Last run after `9b05f61`: both passed. Coverage includes cache, singleflight, translation, HTTP/SOCKS5 proxy, sticky cred, failover, no-secret admin, streaming through proxy.

## Known gaps

- Docker Compose not run on the original Windows machine.
- No live billed generation in this workspace.
- Semantic cache exists but stays off.
- Singleflight waiters do not receive live tokens (they get the completed result / synthesized stream).
