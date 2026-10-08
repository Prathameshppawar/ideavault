# API

Versioned REST under `/v1`, JSON everywhere except SSE chat streams and file downloads.

- **Live reference:** `GET /docs` (Swagger UI) and `GET /v1/openapi.json` on the API. Through the web app: `/api/docs`.
- **Contract in the repo:** [`packages/contracts/openapi.json`](../packages/contracts/openapi.json) is **generated from the Go route registry and types** (`make contracts`), so it cannot drift from the implementation. CI fails if it is stale. The web app's TypeScript types (`apps/web/types/api.generated.ts`) are generated from it.

## Authentication

| Method | Use |
|---|---|
| Session cookie `iv_session` (HttpOnly, SameSite=Lax, Secure in production) | Browser. Set by `POST /v1/auth/setup` (first run only) or `POST /v1/auth/login`. |
| `Authorization: Bearer ivt_…` | Personal API tokens created in Settings (`POST /v1/auth/tokens`, shown once). |

Unsafe requests authenticated by cookie must send `X-IdeaVault-CSRF: 1` (a custom header cannot be sent cross-site without a CORS preflight, which only `WEB_ORIGINS` pass). Bearer requests are exempt. Destructive deletes additionally require `X-Confirm: delete`.

## Errors

```json
{ "error": "branch does not belong to this idea", "kind": "invalid", "field": "branch_id", "request_id": "…" }
```

`kind` → status: `invalid` 400, `unauthorized` 401, `forbidden` 403, `not_found` 404, `conflict`/`immutable` 409, `rate_limited` 429 (with `Retry-After`), `unavailable` 503, `internal` 500 (message is generic; details are in logs under the request id).

## Endpoint groups

| Group | Endpoints |
|---|---|
| auth | `GET /v1/auth/status`, `POST /v1/auth/setup`, `POST /v1/auth/login`, `POST /v1/auth/logout`, `GET /v1/auth/me`, `POST /v1/auth/password`, `GET/DELETE /v1/auth/sessions[/{id}]`, `POST /v1/auth/tokens` |
| ideas | `GET/POST /v1/ideas`, `POST /v1/ideas/check-duplicates`, `GET/PATCH/DELETE /v1/ideas/{id}`, `/versions`, `/journey`, `/tree`, `/conclude`, `/reopen`, `/links`, `/similar`, `/evolution`, `/contradictions` |
| branches | `GET/POST /v1/ideas/{id}/branches`, `GET/PATCH /v1/branches/{id}`, `/inheritance`, `/merge-context`, `GET /v1/branches/compare?a=&b=&explain=` |
| checkpoints | `GET/POST /v1/ideas/{id}/checkpoints`, `GET /v1/checkpoints/{id}`, `POST /v1/checkpoints/{id}/fork`, `GET /v1/checkpoints/compare?from=&to=` |
| knowledge | `GET/POST /v1/knowledge`, `POST /v1/knowledge/review`, `GET /v1/knowledge/{id}` (explanation), `PATCH /v1/knowledge/{id}/status` |
| relationships | `GET/POST /v1/relationships`, `DELETE /v1/relationships/{id}` |
| conversations | `GET /v1/conversations`, `GET/PATCH /v1/conversations/{id}`, `POST /v1/conversations/{id}/extract`, `GET /v1/messages/{id}` (resolves a message's conversation for deep links) |
| agent | `POST /v1/agent/chat` (SSE), `POST /v1/agent/tool-calls/{id}/confirm` (SSE), `GET /v1/agent/pending[?conversation_id=]`, `GET /v1/agent/runs/{id}`, `GET /v1/agent/tools`, `GET/PUT /v1/agent/policy` |
| artifacts | `GET /v1/artifacts/templates`, `GET/POST /v1/artifacts`, `POST /v1/artifacts/generate`, `GET/PATCH/DELETE /v1/artifacts/{id}`, `/regenerate`, `/versions`, `/provenance`, `/explain`, `/download` (`.md`) |
| context packs | `GET/POST /v1/context-packs`, `GET /v1/context-packs/{id}`, `/export?format=md|json` |
| deltas | `GET/POST /v1/deltas`, `GET /v1/deltas/{id}`, `/merge`, `/discard` |
| imports | `GET /v1/imports/adapters`, `GET/POST /v1/imports` (multipart or JSON), `GET /v1/imports/{id}`, `POST /v1/imports/{id}/commit` |
| search | `GET /v1/search?q=&types=&idea_id=&mode=hybrid|fts|semantic&order=` |
| analysis | `POST /v1/analysis/prompt`, `POST /v1/analysis/thinking`, `GET /v1/analysis` |
| dashboard | `GET /v1/dashboard/{today,timeline,decisions,learning,momentum,archive}`, `GET /v1/graph`, `GET /v1/activity` |
| models | `GET/POST /v1/models`, `PATCH/DELETE /v1/models/{id}`, `GET /v1/models/providers`, `PUT/DELETE /v1/models/providers/{p}`, `GET /v1/models/providers/{p}/available`, `GET/PUT /v1/models/routing`, `GET/POST /v1/models/lab`, `GET /v1/models/lab/{id}`, `GET /v1/models/benchmarks` |
| usage | `GET /v1/usage?days=&provider=&model=&task=&idea_id=` |
| connectors | `GET /v1/connectors`, `POST /v1/connectors/{key}/connect`, `POST /v1/connectors/{key}/disconnect`, `GET /v1/connectors/events` |
| settings/system | `GET/PATCH /v1/settings`, `POST /v1/embeddings/backfill`, `GET /v1/system/status`, `GET /healthz`, `GET /readyz` |

## Example

```bash
# sign in (cookie jar)
curl -c jar -H 'Content-Type: application/json' -d '{"email":"me@example.com","password":"…"}' localhost:8080/v1/auth/login
# chat (SSE)
curl -N -b jar -H 'Content-Type: application/json' -H 'X-IdeaVault-CSRF: 1' \
  -d '{"message":"I have an idea for a biker community platform where people can create trips."}' \
  localhost:8080/v1/agent/chat
```

Rate limits per minute (configurable): auth 10, agent 30, imports 10, everything else 600.
