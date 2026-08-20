# AGENTS.md

Self-hosted LLM API gateway (`llmgw`) in front of OpenCode Go, Command Code, and LTN Proxy.

Git = source-code truth. `.agent/` = working-state truth. CLI chat = temporary only.

## Session memory protocol

### Sources of truth

| Layer | Role |
| --- | --- |
| Git | Code. If it is not committed, it is not done. |
| `.agent/` | Goal, progress, issues, next steps, decisions. |
| `docs/PROJECT_HANDOFF.md` | Long-form runtime / schema / safety / policy. |
| CLI conversation | Temporary. Do not treat Claude / Codex / Gemini / OpenCode / GPT chat as memory. |

### Session start

1. Run `python scripts/agent_start.py` (read-only).
2. Follow this file.
3. Read `docs/PROJECT_HANDOFF.md` before material changes.
4. Then read the files you will edit.

### While working

- Keep `.agent/STATE.md` current (Goal / Currently working on / Completed / Current issue / Next).
- Date material decisions in `.agent/DECISIONS.md`.
- Keep `.agent/TODO.md` as P0 / P1 / P2 checkboxes.
- No conversation dumps. No API keys, proxy URLs with passwords, or `.env` values.

### Session end / switching agents

1. Run `python scripts/agent_handoff.py`.
2. Fill `.agent/STATE.md`, `.agent/TODO.md`, and `.agent/HANDOFF.md`.
3. If runtime/schema/safety/policy changed, update `docs/PROJECT_HANDOFF.md` in the same commit.
4. Commit `.agent/` with the work it describes.
5. Keep STATE / TODO / HANDOFF short.

### Do not

- Do not invent provider endpoints.
- Do not put API keys or proxy URLs in cache keys.
- Do not silently fall a proxied credential back to the VPS IP.
- Do not share one `http.Transport` across credentials.
- Do not log raw prompts unless `log_prompts` is on.
