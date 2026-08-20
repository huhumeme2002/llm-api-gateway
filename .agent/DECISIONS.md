# DECISIONS

## 2026-08-19 — Git / `.agent` / chat as three layers

Git is code truth. `.agent/` is working-state truth. Chat is temporary. Canonical long-form handoff lives in `docs/PROJECT_HANDOFF.md`. No conversation dumps, no secrets in these files.

## 2026-08-19 — One `http.Client` per credential, proxy optional and sticky

Each API key owns a long-lived Transport. HTTP/HTTPS/SOCKS5 supported. No `ProxyFromEnvironment`. Dead proxy does not retry that key over the server IP (`direct_fallback: false`). Sticky session maps to `provider|model|credential_id` in Redis. Cache/singleflight stay keyed on tenant+canonical request, never on raw keys or proxy URLs. Credential is chosen once inside the singleflight winner.

## 2026-08-19 — Legacy `api_key_env` becomes credential `default`

Old single-key provider YAML must keep working. `ResolvedCredentials()` synthesizes `{id: default, api_key_env: ...}` when `credentials:` is absent.

## 2026-08-19 — Do not rewrite provider bodies on native hops

Replace only the `model` field (`sjson`) so coding-agent prompt prefixes stay byte-stable for upstream prompt cache.

## 2026-08-19 — VPS binds loopback; Redis stays internal

Compose publishes `127.0.0.1:8080` only. Redis is not published. `GOMEMLIMIT=384MiB`, L1 2k entries on `config.vps.yaml`.
