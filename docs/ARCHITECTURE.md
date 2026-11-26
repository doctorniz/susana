# Architecture

Susanah is a plain monitoring pipeline. The LLM sits off to the side and is only called when there is something to explain.

```
integrations ──► monitor ──► notifiers (Discord, Telegram, ntfy)
                   │  ▲
                   ▼  │
               status page / API      LLM (optional, BYOK)
```

| Package | Role |
| --- | --- |
| `internal/config` | Loads `susanah.yaml`, expands `${VAR}` from the environment |
| `internal/integration` | The plugin interface and registry |
| `internal/integration/arr` | One shared integration for Sonarr, Radarr, Lidarr, Readarr, Prowlarr |
| `internal/integration/httpcheck` | Generic HTTP check for any app, including self-built ones |
| `internal/monitor` | Runs checks on an interval, tracks state, notifies on change |
| `internal/notify` | Send-only chat channels |
| `internal/llm` | Provider interface: Anthropic and any OpenAI-compatible API |
| `internal/server` | Status page, `/api/status`, `/healthz` |

## Principles

1. **Read-only.** v1 integrations only observe. Actions (restarts and so on) will come later, behind an explicit approval step and an audit log.
2. **Useful without an LLM.** Every alert works with no model configured.
3. **Untrusted text stays data.** Logs, media titles and app messages can be written by anyone; they are wrapped as data in prompts and can never trigger actions.
4. **Alert on change.** A check notifies when its status changes, not on every run.

## Adding an integration

1. Create `internal/integration/<name>/<name>.go`.
2. Implement `integration.Integration` (`Name`, `Kind`, `Check`).
3. Call `integration.Register("<kind>", New)` in an `init` func.
4. Import the package in `cmd/susanah/main.go`.
5. Add tests using `httptest` with a recorded response from the real app.

## Roadmap

- **v0.1:** *arr apps, generic HTTP, Discord/Telegram/ntfy, LLM explanations (this scaffold)
- **v0.2:** Docker socket, host metrics, Proxmox API, Jellyfin, Home Assistant, Audiobookshelf, n8n
- **v0.3:** SQLite history, incident grouping, Telegram/Discord chat (ask questions)
- **v0.4:** Remote satellites, Suwayomi, qBittorrent, BookOrbit, Hermes Agent
- **Later:** approved actions, weekly digest, community plugin registry
