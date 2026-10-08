# Connectors

Connectors give the agent access to outside systems through tools. Their status is always **real**: a connector shows `CONNECTED` only after IdeaVault verified the credentials (or, for keyless connectors, after you granted permission).

| Connector | Auth | Status in this build | Tools (category) |
|---|---|---|---|
| **Web** | none | implemented | `web_fetch` (EXTERNAL) — public pages through the SSRF-safe fetcher; content is untrusted |
| **GitHub** | personal access token | implemented | `github_list_repos`, `github_get_readme` (EXTERNAL), `github_create_issue` (EXTERNAL action) |
| **Custom HTTP** | base URL | implemented | `custom_get` (EXTERNAL) — GET under your configured public base URL |
| Google Drive, Gmail, Slack, Google Calendar, Notion | OAuth 2 | definition only | Require registering an OAuth application with the provider and server-side client credentials; the UI states this honestly and never fakes a connection. |
| ChatGPT, Claude, Gemini | export | via Import Center | These platforms offer no API for reading your conversations; use the official export or public share links. Passwords are never requested. |

## Lifecycle

`POST /v1/connectors/{key}/connect {credentials, permissions}` → verify (e.g. `GET https://api.github.com/user`) → credentials stored **AES-256-GCM encrypted** (`credentials` table; key from `MASTER_KEY`, additional data binds ciphertext to user+connector+field) → state `CONNECTED` with granted permissions, or `ERROR` with the (redacted) reason. `POST …/disconnect` deletes credentials.

Every invocation is checked (connected? permission granted?) and recorded in `connector_events` (tool, operation, success, latency, error, agent run) — visible on the Usage page.

## Permissions

Connector tools are in the **EXTERNAL** category: the agent must ask for confirmation before each call unless you enable "Let connectors run without asking" in Settings → Agent permissions. Granted permissions (e.g. `github.read` vs `github.issues.write`) further restrict what a connected connector may do.

## Adding a connector

Implement `connectors.Connector` (`Definition`, `Verify`, `Invoke`) in `apps/api/internal/connectors`, register it in `NewRegistry`, declare its tools with JSON-Schema parameters, category and permission. The agent picks the tools up automatically (`connectorTools`), and they appear in the UI and the tool catalog.
