# Susanah

**A self-hosted monitoring agent for your homelab that tells you what broke and why.**

Susanah watches your self-hosted apps and the servers they run on, then sends alerts to Discord, Telegram or ntfy. Bring your own LLM key and every alert also gets a short, plain-language explanation of the likely cause and what to do next.

- **App-aware checks.** Not just "port 8989 answers": Susanah reads each app's own health API, such as Sonarr's missing root folder or Prowlarr's failing indexers.
- **Works without AI.** Plain alerts work out of the box. Add an LLM key to make them smarter.
- **Bring your own key.** Anthropic, OpenAI, OpenRouter, Groq, or any OpenAI-compatible API, including a local Ollama.
- **Read-only.** v1 never changes anything in the apps it watches.
- **One container**, a single Go binary, MIT licensed.

> Status: early development. Expect breaking changes until v1.0.

## Quick start

```sh
mkdir -p susanah/config && cd susanah
curl -fsSLO https://raw.githubusercontent.com/doctorniz/susana/main/docker-compose.yml
curl -fsSL https://raw.githubusercontent.com/doctorniz/susana/main/config.example.yaml -o config/susanah.yaml
touch .env        # put SONARR_API_KEY=..., DISCORD_WEBHOOK_URL=... here
docker compose up -d
```

Open http://localhost:8686 for the status page. `GET /api/status` returns the same data as JSON.

## Configuration

Everything lives in `config/susanah.yaml`; see [config.example.yaml](config.example.yaml). Any `${VAR}` is read from the environment, so secrets can stay in `.env`.

### Integrations

| Kind | Apps | Checks |
| --- | --- | --- |
| `sonarr`, `radarr`, `lidarr`, `readarr`, `prowlarr` | The *arr apps | Reachability, API key, every warning from the app's own health page |
| `http` | Anything with an HTTP endpoint, including your own apps | Status code, body text, or a JSON field value |

On the roadmap: Proxmox, Docker, host metrics, Jellyfin, Home Assistant, Audiobookshelf, n8n, Suwayomi, qBittorrent, BookOrbit, Hermes Agent and more.

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

Susanah sends the alert and the status of your other apps; it never sends your API keys. Text from monitored apps is marked as data in the prompt.

## Development

```sh
make test    # go test -race ./...
make lint    # gofmt + go vet
make run     # build and run with config/susanah.yaml
```

New integrations go in `internal/integration/<name>` and register themselves with `integration.Register`. See [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md).

## License

[MIT](LICENSE)
