# HANDOFF

## Last agent

Grok (this session). Added the shared `.agent/` memory layer and scripts.

## Changed

- `AGENTS.md` — session memory protocol
- `.agent/STATE.md`, `DECISIONS.md`, `TODO.md`, `HANDOFF.md`
- `docs/PROJECT_HANDOFF.md` — long-form canonical handoff
- `scripts/agent_start.py` (read-only bootstrap)
- `scripts/agent_handoff.py` (archive HANDOFF + print git status/diff)

Code of the gateway itself was **not** changed in this step. Last code commit remains `9b05f61` (per-credential keys + dedicated proxies).

## Still broken

- Docker Compose unverified on the Windows workspace (Docker not installed)
- No live billed chat test (no provider keys in the repo)
- Unrelated “scroll photos / swipe, no click” request is **not this codebase** (this repo has no image viewer UI)

## Next

1. New agent: `python scripts/agent_start.py`, then read `docs/PROJECT_HANDOFF.md` before material edits.
2. If continuing product work: VPS/docker smoke test, or P2 README tone — do not start from chat history.

## Do not

- Do not commit `.env`, `config.yaml`, or keys
- Do not dump CLI transcripts into `.agent/`
- Do not share Transports across credentials or fall proxied keys back to direct IP
- Do not treat chat as recoverable state
