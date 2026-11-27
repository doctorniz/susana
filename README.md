# Susana

**A self-hosted monitoring agent for your homelab that tells you what broke and why.**

Susana watches your self-hosted apps and the servers they run on, then sends alerts to Discord, Telegram or ntfy. Bring your own LLM key and every alert also gets a short, plain-language explanation of the likely cause and what to do next.

- **App-aware checks.** Not just "port 8989 answers": Susana reads each app's own health API, such as Sonarr's missing root folder or Prowlarr's failing indexers.
- **Works without AI.** Plain alerts work out of the box. Add an LLM key to make them smarter.
- **Bring your own key.** Anthropic, OpenAI, OpenRouter, Groq, or any OpenAI-compatible API, including a local Ollama.
- **Read-only.** v1 never changes anything in the apps it watches.
- **One container**, a single Go binary, MIT licensed.

> Status: early development. Expect breaking changes until v1.0.

## Quick start

```sh
mkdir -p susana/config && cd susana
curl -fsSLO https://raw.githubusercontent.com/doctorniz/susana/main/docker-compose.yml
curl -fsSL https://raw.githubusercontent.com/doctorniz/susana/main/config.example.yaml -o config/susana.yaml
touch .env        # put SONARR_API_KEY=..., DISCORD_WEBHOOK_URL=... here
docker compose up -d
```

Open http://localhost:8686 for the status page.

## Configuration

Everything lives in `config/susana.yaml`; see [config.example.yaml](config.example.yaml). Any `${VAR}` is read from the environment, so secrets can stay in `.env`.

### Integrations

| Kind | Apps | Checks |
| --- | --- | --- |
| `proxmox` | Proxmox VE node or cluster | Nodes online, CPU and memory pressure, VMs and LXCs that should be running, storage usage |
| `host` | The Linux machine Susana runs on | Load per core, memory, free space on chosen mounts |
| `docker` | Docker containers | Unhealthy, restarting, crashed (non-zero exit), or expected-but-missing containers |
| `sonarr`, `radarr`, `lidarr`, `readarr`, `prowlarr` | The *arr apps | Reachability, API key, every warning from the app's own health page |
| `jellyfin` | Jellyfin | Reachability, API key, pending restart, failed scheduled tasks, active and transcoding streams |
| `homeassistant` | Home Assistant | Core state, safe or recovery mode, chosen entities going unavailable, unavailable count, pending updates |
| `audiobookshelf` | Audiobookshelf | Reachability, setup, API token, libraries without folders, optional backup age |
| `n8n` | n8n | Readiness (database and migrations), failed workflow executions in a time window, active workflows |
| `http` | Anything with an HTTP endpoint, including your own apps | Status code, body text, or a JSON field value |

Thresholds are configurable per integration; see [config.example.yaml](config.example.yaml). On the roadmap: remote hosts via satellites, Proxmox Backup Server, SMART and temperatures, Suwayomi, qBittorrent, BookOrbit, Hermes Agent and more.

### Notifiers

`discord` (webhook), `telegram` (bot token + chat id) and `ntfy`. Each can set `min_level: critical` to stay quiet about warnings. Recoveries are always sent.

### LLM (optional)

```yaml
llm:
  provider: anthropic        # or openai, for any OpenAI-compatible API
  api_key: ${LLM_API_KEY}
  model: your-model-id
  # base_url: http://ollama:11434/v1
```

Susana sends the alert and the status of your other apps; it never sends your API keys. Text from monitored apps is marked as data in the prompt.

## API and MCP (for AI agents)

Susana exposes its state so other tools and AI agents can ask about your homelab. Everything is read-only. Set `api_token` in the config (or `SUSANA_API_TOKEN` via `${...}`) and send it as `Authorization: Bearer <token>`.

### HTTP API

| Endpoint | Returns |
| --- | --- |
| `GET /api/summary` | Overall status, counts by status, and every app that is not OK |
| `GET /api/status` | Every app with its latest check results, worst first |
| `GET /api/apps/{name}` | One app |
| `GET /api/events?app=&limit=` | Recent status changes, newest first |
| `GET /healthz` | `ok` (no token needed) |

### MCP server

Susana is an MCP server at `http://<host>:8686/mcp` (Streamable HTTP). Tools:

| Tool | What it answers |
| --- | --- |
| `get_summary` | Is anything broken right now? |
| `list_apps` | Status of everything |
| `get_app` | What is wrong with one app? |
| `list_events` | What changed, and when? |

Point any MCP client at it, such as Hermes Agent, Claude Desktop or Claude Code, n8n or Open WebUI, with the bearer token as a header. For example, Claude Code:

```sh
claude mcp add --transport http susana http://susana:8686/mcp --header "Authorization: Bearer $SUSANA_API_TOKEN"
```

For Hermes Agent, add Susana as an HTTP MCP server in its config with the same URL and `Authorization` header (see the Hermes docs for the exact format).

## Development

```sh
make test    # go test -race ./...
make lint    # gofmt + go vet
make run     # build and run with config/susana.yaml
```

New integrations go in `internal/integration/<name>` and register themselves with `integration.Register`. See [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md).

## License

[MIT](LICENSE)
