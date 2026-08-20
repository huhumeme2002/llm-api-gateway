# AGENTS.md

This repo is a self-hosted LLM API gateway (`llmgw`) in front of **OpenCode Go** and **Command Code**.

Git is the source of the code. `.agent/` is the source of working state. Chat transcripts are temporary and are never the source of truth.

## Session memory protocol

### Sources of truth

| Layer | Role |
| --- | --- |
| Git | Source-code truth. If it is not committed, it is not done. |
| `.agent/` | Working-state truth: goal, progress, issues, next steps, decisions. |
| `docs/PROJECT_HANDOFF.md` | Long-form canonical handoff (runtime, schema, safety, policy). |
| CLI conversation | Temporary only. Do not treat Claude / Codex / Gemini / OpenCode chat as memory. |

### Session start

1. Run `python scripts/agent_start.py` (read-only).
2. Follow this file.
3. Read `docs/PROJECT_HANDOFF.md` **before** any material change (runtime, schema, routing, cache, credentials, safety, public API).
4. Then read the files you will actually edit.

### While working

- Keep `.agent/STATE.md` current: Goal / Currently working on / Completed / Current issue / Next.
- Log material decisions (and why) in `.agent/DECISIONS.md` with a date.
- Keep `.agent/TODO.md` as P0 / P1 / P2 checkboxes. Check items off when git has the work.
- Do not dump conversations into `.agent/`.
- Do not put secrets, API keys, proxy URLs with passwords, or `.env` values into `.agent/` or docs.

### Session end / switching agents

1. Run `python scripts/agent_handoff.py`.
2. Fill `.agent/STATE.md`, `.agent/TODO.md`, and `.agent/HANDOFF.md`.
3. If you changed runtime, schema, safety, or policy, update `docs/PROJECT_HANDOFF.md` in the **same** commit.
4. Commit `.agent/` together with the work it describes.
5. Keep STATE / TODO / HANDOFF short and structured.

### Do not

- Do not invent provider endpoints. Use the public docs and live `/models` catalogs.
- Do not put API keys or proxy URLs in cache keys.
- Do not silently fall a proxied credential back to the VPS IP (`proxy.direct_fallback: false`).
- Do not share one `http.Transport` across credentials.
- Do not log raw prompts unless `log_prompts` is on.
- Do not treat a chat session as recoverable memory.

## Quick map

- `cmd/gateway` — process entry
- `internal/api` — HTTP surface
- `internal/provider` — OpenCode Go / Command Code + per-credential HTTP clients
- `internal/cache` + `internal/singleflight` — L1/L2 exact cache and coalescing
- `internal/router` — sticky session, circuit breaker, credential order
- `config.example.yaml` / `config.vps.yaml` — config shapes
- Tests: `go test ./...` and `go test -race ./...` (race needs CGO + gcc on Windows)
