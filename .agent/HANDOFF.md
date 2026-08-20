# HANDOFF

## Last agent

Grok. Ran `python scripts/agent_handoff.py`, filled STATE/TODO/HANDOFF.

## Changed

- Restored session memory: `AGENTS.md`, `scripts/agent_start.py`, `scripts/agent_handoff.py`, `docs/PROJECT_HANDOFF.md`, `.agent/`
- Prior code (already on `main`): LTN Proxy provider; ROUND 1 LTN bench artifacts stay local under `bench/results/`
- `.gitignore` ignores `bench/results/`

## Still broken

- Docker Compose unverified on this Windows box
- `qwen3.8-max` blocked by LTN team Alibaba allowlist
- No live billed request through the **gateway** in this workspace
- ROUND 2 not run

## Next

1. New session: `python scripts/agent_start.py` then `docs/PROJECT_HANDOFF.md`
2. Product: gateway smoke with LTN, or ROUND 2 if requested

## Do not

- Do not commit `.env`, `config.yaml`, keys, or `bench/results/`
- Do not dump chat transcripts into `.agent/`
- Do not share Transports across credentials or fall proxied keys back to direct IP
- Do not invent provider endpoints
