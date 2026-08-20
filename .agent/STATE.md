# STATE

## Goal

Self-hosted LLM gateway in front of OpenCode Go, Command Code, and LTN Proxy, with cache, singleflight, sticky sessions, and one dedicated outbound proxy per API key.

## Currently working on

Handoff after adding LTN Proxy and finishing LTN ROUND 1 bench.

## Completed

- Gateway: `/v1/models`, chat/completions, responses, messages
- Providers: opencode_go, commandcode, **ltnproxy** (`f392eea`)
- Per-credential HTTP clients + optional HTTP/HTTPS/SOCKS5 proxies
- L1/L2 cache, singleflight, sticky `x-session-id`, circuit failover
- VPS compose/systemd defaults
- LTN ROUND 1 bench (9/19 models alive); report in `bench/results/` (gitignored)

## Current issue

None blocking. `alibaba/qwen3.8-max` 502 = LTN team restricted Alibaba, not a gateway bug. Docker Compose never verified on the original Windows machine.

## Next

1. Optional ROUND 2 bench on ~8 live LTN models
2. Smoke a real chat through the **gateway** with `LTN_API_KEY` in env only
3. Rotate any key that was pasted in chat
