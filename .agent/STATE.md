# STATE

## Goal

Ship a local/VPS LLM API gateway that fronts OpenCode Go and Command Code behind one OpenAI- and Anthropic-compatible API, with cache, singleflight, sticky routing, and one dedicated outbound proxy per API key.

## Currently working on

Nothing in flight. Last shipped work: per-credential keys + dedicated proxies (`9b05f61`).

## Completed

- Unified `/v1/models`, `/v1/chat/completions`, `/v1/responses`, `/v1/messages`
- L1 LRU + Redis L2 exact cache; stream cache; tenant isolation
- Singleflight (local + Redis); cache keys do not include secrets
- Sticky session (`x-session-id`) for provider/model **and** credential
- Circuit breaker + failover per credential; no direct-IP fallback for proxied keys
- VPS compose/systemd/Caddy defaults; admin `/admin/credentials`
- Tests + race green on last credential/proxy commit
- Public repo: https://github.com/huhumeme2002/llm-api-gateway (`main`)

## Current issue

None blocking. Docker Compose was never verified on the original Windows machine (no Docker). Live chat with real provider keys was not run here (no secrets in the workspace).

## Next

1. Keep this memory layer in git and use it at session start/end.
2. On a VPS: `docker compose up` and a real chat smoke test with keys in `.env` only.
3. Optional: friendlier README/error copy (asked, then superseded; not started in code).
