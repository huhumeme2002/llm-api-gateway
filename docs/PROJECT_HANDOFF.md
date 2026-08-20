# Project handoff — llm-api-gateway

Canonical long-form handoff. Update this file in the same commit as runtime, schema, safety, or policy changes.

Repo: https://github.com/huhumeme2002/llm-api-gateway  
Module: `llmgw`

## What it is

Self-hosted gateway:

```
GET  /v1/models
POST /v1/chat/completions
POST /v1/responses
POST /v1/messages
```

Client: `base_url=http://127.0.0.1:8080/v1`, `Authorization: Bearer $GATEWAY_API_KEY`.

## Providers

| Name | Base | Key env | Native |
| --- | --- | --- | --- |
| OpenCode Go | `https://opencode.ai/zen/go/v1` | `OPENCODE_GO_API_KEY` | Chat / Messages (MiniMax,Qwen) / Responses (`grok-4.5`,`gpt-5.6-luna`) |
| Command Code | `https://api.commandcode.ai/provider/v1` | `COMMANDCODE_API_KEY` | Chat; Claude → `/messages` only |
| LTN Proxy | `https://ltnproxy.com/v1` | `LTN_API_KEY` | Chat Completions; Messages/Responses translated |

Client IDs: `opencode-go/<id>`, `commandcode/<id>`, `ltnproxy/<id>` (LTN ids may contain slashes). Aliases: `default`,`fast`,`cheap`,`coding`,`reasoning`.

## Runtime

Go 1.23+, Redis for L2/locks/sticky. `make run` or `docker compose up` (loopback `:8080`, Redis internal). VPS: `config.vps.yaml`, `GOMEMLIMIT=384MiB`. Secrets only in `.env` (gitignored).

## Credentials / proxies

Each credential: own `http.Client`. Optional proxy via `proxy_env`. `proxy.direct_fallback: false`. Sticky `x-session-id` → `provider|model|credential_id`. Failover before first stream byte only. Cache key never includes keys/proxy URLs.

## Safety

No prompt logs by default. Redact secrets in errors/admin. `/admin/credentials` returns ids and stats only. Do not invent endpoints or bypass provider 429s.

## Tests

`go test ./...` — last full run green after LTN provider add (`f392eea`).

## Benchmark (optional, `bench/`)

ROUND 1 against LTN (2026-08-20): 9/19 models alive. Grok 4.6/4.5 best quality/tender; DeepSeek pro-0813 fastest; Qwen 3.8 2.4T best prefix cache. `alibaba/qwen3.8-max` 502 team-restricted Alibaba. ROUND 2 not run. Results under `bench/results/` (gitignored).

## Gaps

Docker Compose unverified on original Windows machine. Live chat through the **gateway** with all three keys not smoked. Semantic cache off.
