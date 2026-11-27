# Architecture

Susana is a plain monitoring pipeline. The LLM sits off to the side and is only called when there is something to explain.

```
integrations ──► monitor ──► notifiers (Discord, Telegram, ntfy)
                   │  ▲
                   ▼  │
        status page / API / MCP       LLM (optional, BYOK)
```

| Package | Role |
| --- | --- |
| `internal/config` | Loads `susana.yaml`, expands `${VAR}` from the environment |
| `internal/integration` | The plugin interface and registry |
| `internal/integration/arr` | One shared integration for Sonarr, Radarr, Lidarr, Readarr, Prowlarr |
| `internal/integration/proxmox` | Proxmox VE nodes, guests and storage via a read-only API token |
| `internal/integration/host` | Load, memory and disk of a Linux host from `/proc` and `statfs` |
| `internal/integration/docker` | Container state via the Engine API (socket or socket proxy) |
| `internal/integration/jellyfin` | Jellyfin server state, scheduled tasks and streams |
| `internal/integration/homeassistant` | Home Assistant core state, watched entities and updates |
| `internal/integration/audiobookshelf` | Audiobookshelf status, libraries and backups |
| `internal/integration/n8n` | n8n readiness and failed workflow executions |
| `internal/integration/apiclient` | Shared JSON-over-HTTP helper for the app integrations |
| `internal/integration/httpcheck` | Generic HTTP check for any app, including self-built ones |
| `internal/monitor` | Runs checks on an interval, tracks state, notifies on change |
| `internal/notify` | Send-only chat channels |
| `internal/llm` | Provider interface: Anthropic and any OpenAI-compatible API |
| `internal/server` | Status page, JSON API under `/api`, `/healthz`, bearer-token auth |
| `internal/mcpserver` | Read-only MCP tools for AI agents, served at `/mcp` |

## Principles

1. **Read-only.** v1 integrations only observe. Actions (restarts and so on) will come later, behind an explicit approval step and an audit log.
2. **Useful without an LLM.** Every alert works with no model configured.
3. **Untrusted text stays data.** Logs, media titles and app messages can be written by anyone; they are wrapped as data in prompts and can never trigger actions.
4. **Alert on change.** A check notifies when its status changes, not on every run.

## Adding an integration

1. Create `internal/integration/<name>/<name>.go`.
2. Implement `integration.Integration` (`Name`, `Kind`, `Check`).
3. Call `integration.Register("<kind>", New)` in an `init` func.
4. Import the package in `cmd/susana/main.go`.
5. Add tests using `httptest` with a recorded response from the real app.

## Roadmap

- **v0.1:** *arr apps, generic HTTP, Discord/Telegram/ntfy, LLM explanations (this scaffold)
- **v0.1.x:** read-only HTTP API and MCP server (done)
- **v0.1.x:** Proxmox, host and Docker checks (done)
- **v0.1.x:** Jellyfin, Home Assistant, Audiobookshelf and n8n checks (done)
- **v0.2:** Proxmox Backup Server, SMART and temperatures
- **v0.3:** SQLite history, incident grouping, Telegram/Discord chat (ask questions)
- **v0.4:** Remote satellites, Suwayomi, qBittorrent, BookOrbit, Hermes Agent
- **Later:** approved actions, weekly digest, community plugin registry
