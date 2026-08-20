# TODO

## P0

- [x] Gateway compiles, `go test ./...` and `go test -race ./...` green after credential/proxy work
- [x] Shared multi-CLI session memory (`.agent/`, `AGENTS.md`, start/handoff scripts)

## P1

- [ ] Smoke `docker compose up` on a machine that has Docker
- [ ] One real OpenAI-compat chat through the gateway with keys only in `.env`
- [ ] Confirm sticky `x-session-id` pins the same credential across two live requests

## P2

- [ ] Friendlier README / operator error copy without touching the hot path
- [ ] Durable usage beyond Redis (SQLite/Postgres) if monthly budgets need history
- [ ] Live singleflight stream fan-out (waiters currently wait, then synthesize)
