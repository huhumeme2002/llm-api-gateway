# DECISIONS

## 2026-08-20 — Restore `.agent/` on this repo

User asked to run handoff.py → fill STATE/TODO/HANDOFF → commit. The layer had been deleted as “wrong project”; it is back on this gateway repo.

## 2026-08-20 — LTN Proxy is a first-class provider

`https://ltnproxy.com/v1`, Bearer `LTN_API_KEY`, Chat Completions only natively. Client IDs `ltnproxy/<upstream>` including nested IDs (`ltnproxy/deepseek/deepseek-v4-flash`). Team allowlists can 502 Alibaba-routed models.

## 2026-08-19 — One HTTP client per credential

Dedicated Transport + optional HTTP/HTTPS/SOCKS5 proxy. `direct_fallback: false`. Cache keys never include secrets. Sticky Redis: `provider|model|credential_id`.

## 2026-08-19 — Native hops only replace `model`

`sjson` only, so coding-agent prompt prefixes stay byte-stable.
