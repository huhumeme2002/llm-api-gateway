# LLM API Gateway

Self-hosted gateway that fronts **OpenCode Go** and **Command Code** behind one OpenAI- and Anthropic-compatible API.

```text
http://localhost:8080/v1/models
http://localhost:8080/v1/chat/completions
http://localhost:8080/v1/responses
http://localhost:8080/v1/messages
```

Verified against official docs on 2026-08-19:

- OpenCode Go: https://opencode.ai/docs/go/ — base `https://opencode.ai/zen/go/v1`
- Command Code Provider API: https://commandcode.ai/docs/provider — base `https://api.commandcode.ai/provider/v1`

## Start (local)

```bash
cp .env.example .env
# set OPENCODE_GO_API_KEY, COMMANDCODE_API_KEY, GATEWAY_API_KEY

docker compose up -d --build
# or: make run
```

Compose binds the API to **127.0.0.1:8080** and keeps Redis on the internal network. Put Caddy/nginx in front on a VPS.

## VPS (1–2 GB)

Tuned for a small Linux VPS:

- `GOMEMLIMIT=384MiB` so Go GC stays inside ~512 MB
- L1 cache 2k entries (see `config.vps.yaml`)
- Redis `maxmemory 128mb` + `allkeys-lru`, not published to the internet
- Gateway listen on loopback; Caddy terminates TLS
- systemd unit with `MemoryMax=512M`, `LimitNOFILE=65535`
- Multi-arch image (`amd64` / `arm64` — Oracle Ampere etc.)
- Long streams no longer die on `http.Client` timeout; only dial / first-byte / request context
- `/metrics` requires the admin key (`metrics_public: false`)
- Redis connect retries so compose boot order is safe
- 2-minute graceful shutdown so in-flight generations finish

**Docker on the VPS**

```bash
git clone https://github.com/huhumeme2002/llm-api-gateway.git
cd llm-api-gateway
cp .env.example .env
# fill keys in .env
docker compose up -d --build
# optional TLS:
#   sudo apt install caddy
#   export LLMGW_DOMAIN=gw.example.com
#   sudo cp deploy/Caddyfile /etc/caddy/Caddyfile
#   sudo systemctl reload caddy
```

Open only 80/443 on the firewall (`ufw allow 80,443/tcp`). Do **not** expose 6379.

**Binary + systemd** (no Docker)

```bash
sudo ./deploy/install.sh
sudo $EDITOR /opt/llmgw/.env
sudo systemctl start llmgw
curl -s http://127.0.0.1:8080/health
```

Cross-compile from another machine: `make linux` → `bin/gateway-linux-amd64` and `bin/gateway-linux-arm64`.

## Multiple keys and proxies

Each provider can have many credentials. **Every key gets its own `http.Client` / `Transport` and (optional) outbound proxy.** Proxies are never swapped on a shared client.

```yaml
proxy:
  direct_fallback: false   # dead proxy does not fall back to the VPS IP

providers:
  opencode_go:
    credentials:
      - id: go-01
        api_key_env: OPENCODE_GO_KEY_01
        proxy_env: OPENCODE_GO_PROXY_01
      - id: go-02
        api_key_env: OPENCODE_GO_KEY_02
        proxy_env: OPENCODE_GO_PROXY_02
```

```env
OPENCODE_GO_KEY_01=xxx
OPENCODE_GO_PROXY_01=http://user:pass@1.2.3.4:8080
OPENCODE_GO_KEY_02=xxx
OPENCODE_GO_PROXY_02=socks5://user:pass@5.6.7.8:1080
```

`x-session-id` pins a session to the same credential (and therefore the same proxy) while it is healthy. Failover is per-credential via the circuit breaker. `GET /admin/credentials` shows IDs and health — never keys or proxy URLs.

Legacy `api_key_env` still works and becomes credential `default`.

## Credentials

**OpenCode Go** — subscribe at [opencode.ai/auth](https://opencode.ai/auth), copy the key, set `OPENCODE_GO_API_KEY` (or `OPENCODE_API_KEY`). Same Zen/Go key. Auth header: `Authorization: Bearer`.

**Command Code** — Studio → API keys. Needs GOAT/Pro/Max/Team/Provider (the Command Code *Go* plan has **no** Provider API). Set `COMMANDCODE_API_KEY`.

**Gateway** — clients send `Authorization: Bearer $GATEWAY_API_KEY` (Anthropic clients may use `x-api-key`).

## Curl

```bash
export GW=http://localhost:8080
export KEY=change-me-gateway-key

curl -s $GW/v1/models -H "Authorization: Bearer $KEY" | head

curl -s $GW/v1/chat/completions \
  -H "Authorization: Bearer $KEY" \
  -H "Content-Type: application/json" \
  -H "x-session-id: sess-1" \
  -d '{
    "model": "opencode-go/deepseek-v4-flash",
    "temperature": 0,
    "messages": [{"role":"user","content":"say ping"}]
  }'
```

Repeat the same body: first response has `x-cache: MISS`, second `x-cache: HIT-L1` or `HIT-REDIS`.

Aliases: `default`, `fast`, `cheap`, `coding`, `reasoning` (see `config.yaml`).

```bash
curl -s $GW/v1/messages \
  -H "Authorization: Bearer $KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "commandcode/claude-sonnet-4-6",
    "max_tokens": 128,
    "messages": [{"role":"user","content":"say ping"}]
  }'
```

## SDK

```python
from openai import OpenAI
client = OpenAI(base_url="http://localhost:8080/v1", api_key="change-me-gateway-key")
print(client.chat.completions.create(
    model="fast",
    temperature=0,
    messages=[{"role": "user", "content": "ping"}],
))
```

```python
from anthropic import Anthropic
client = Anthropic(base_url="http://localhost:8080", api_key="change-me-gateway-key")
print(client.messages.create(
    model="commandcode/claude-sonnet-4-6",
    max_tokens=128,
    messages=[{"role": "user", "content": "ping"}],
))
```

## Cache headers

`x-cache` is `HIT-L1` | `HIT-REDIS` | `MISS` | `BYPASS`. Also: `x-gateway-provider`, `x-gateway-model`, `x-cache-key`, `x-upstream-latency-ms`, `x-request-id`, `x-prefix-hash`, `x-prefix-reuse-ratio`.

Override with `x-cache-mode: auto|bypass|force`.

## Admin

```bash
curl -s $GW/health
curl -s $GW/ready
curl -s $GW/metrics
curl -s $GW/admin/cache/stats -H "Authorization: Bearer $KEY"
curl -s $GW/admin/providers -H "Authorization: Bearer $KEY"
curl -X DELETE $GW/admin/cache -H "Authorization: Bearer $KEY"
```

## Tests

```bash
make test
make race
make benchmark
```

## Layout

See `DESIGN.md` and `cmd/gateway`, `internal/{api,auth,cache,canonical,provider,protocol,router,singleflight}`.

Agents: `python scripts/agent_start.py` then `AGENTS.md`. Working state is `.agent/`, not chat. Canonical handoff: `docs/PROJECT_HANDOFF.md`.
