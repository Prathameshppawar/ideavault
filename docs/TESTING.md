# Testing

IdeaVault is tested at five levels. Each runs locally with one command and in CI on every push.

| Level | What it proves | Where | Command |
|---|---|---|---|
| Go unit | Pure logic: domain rules, model gateway/routing/fallback, provider adapters (recorded HTTP), import adapters, injection scanner, security primitives, log redaction | `apps/api/internal/**/*_test.go` | `make test-api` |
| Go integration | Repositories, migrations, services and HTTP handlers against **real PostgreSQL + pgvector** | `apps/api/internal/integration`, `internal/handler/http` | `make test-api` (needs `make up`) |
| Agent evaluation | The supervisor chooses the right tools, respects permissions and answers from the vault | `apps/api/internal/agent`, `internal/evals` | `make eval` |
| Web unit | Rendering, SSE parsing, entity links, delta/import/artifact logic | `apps/web/tests/unit` (Vitest + Testing Library) | `make test-web` |
| End-to-end | The critical user workflows through the real UI, API and database | `apps/web/tests/e2e` (Playwright) | `make e2e` |

`make test` runs Go and web unit/integration tests; `make lint` runs gofmt, go vet, ESLint and `tsc`.

## Databases

Integration tests and E2E never touch your development data:

| Database | Used by | Lifecycle |
|---|---|---|
| `ideavault` | `make api` / `make web` | yours |
| `ideavault_test` | Go integration tests (`TEST_DATABASE_URL`) | migrated once per package run; each test creates its own user, so tests are independent |
| `ideavault_e2e` | Playwright | **reset** (all migrations down, then up) every run |

Both test databases are created by `infra/postgres/init/01-test-db.sql` when the container first starts. If you created the volume before that file existed: `make reset-db`.

Without `TEST_DATABASE_URL` the DB-backed Go tests **skip** (they say so); unit tests still run. CI always provides a database, so nothing is skipped there.

```bash
make up                 # PostgreSQL + Redis
make test-api           # unit + integration
cd apps/api && go test ./internal/integration -run TestCheckpoint -v   # one area
```

## What the integration tests cover

- **Migrations** – every migration applies and rolls back cleanly (`make migrate-verify` does the same from the CLI: up → down → up).
- **Repositories** – tenant isolation (every query is scoped by user), class-table knowledge inheritance, label numbering per idea, full-text and vector search.
- **Ideas and knowledge** – duplicate detection, provenance (SOURCE vs INTERPRETATION, source message), review states, decision supersession/reversal chains with history intact.
- **Checkpoints and branches** – checkpoints are immutable (the database trigger rejects updates), snapshots are content-hashed, forks copy exactly the checkpoint's state, selective forks bring only chosen later items with inheritance records, original branches never change.
- **Imports** – every adapter against fixtures in `tests/fixtures/imports` (ChatGPT export and share page, Claude, Gemini, Markdown, JSON, text, URL), current-branch-only threading, tool output excluded, dedupe by content hash, injection flags, resumable jobs.
- **Deltas and views** – external conversation analysis classifies NEW / CONFIRMS / CHANGES / CONTRADICTS; merge never overwrites, creates a merge checkpoint; dashboard view queries.
- **Artifacts and conclusion** – template generation with provenance, versioning (edits create versions, history kept), Markdown download, conclusion outcomes.
- **HTTP** – auth (setup, login, sessions, CSRF header), destructive deletes require `X-Confirm: delete`, rate limits, error envelope, OpenAPI document generation.

## Agent evaluation

The agent is evaluated against a fixed set of cases (`apps/api/internal/evals/benchmarks.go`): each case is a user message plus a seeded vault, and is scored on tool choice, required arguments, permission behaviour (destructive actions must stop for confirmation; imported text must never trigger tools) and whether the answer cites the vault.

```bash
make eval                                   # offline planner, deterministic — runs in CI
EVAL_MODELS=groq/openai/gpt-oss-120b,groq/openai/gpt-oss-20b make eval-models   # real models, needs keys
```

`make eval-models` calls real providers and costs money (small amounts on Groq); it is never run in CI. Results also appear in **Models → Model Lab**, where the same benchmark can be run from the UI.

## End-to-end (Playwright)

`make e2e` starts an isolated stack and runs the suite against it:

- **API** on `:8090` with `MOCK_AI=true` (the deterministic offline planner, so results are reproducible and no keys are needed), database `ideavault_e2e` reset on start, generous rate limits.
- **Web** on `:3100` (`next dev` locally, `next build && next start` in CI), build output in `.next-e2e` so it never collides with your dev server on `:3000`.
- `auth.setup.ts` creates the vault owner through the real setup screen once; every spec reuses that session.

The flows (`apps/web/tests/e2e`):

| Spec | Flow |
|---|---|
| `chat-flows` | "I have a new idea" → onboarding question → Idea Space with Main branch → decision recorded with source provenance → checkpoint → message deep link → idea page |
| | Continue an idea from checkpoint 4 |
| | Fork from CP4 bringing research from CP7 — selective inheritance, original branch unchanged |
| | "Why did we decide…" after a decision reversal — rationale shown, old decision kept as REVERSED in the chain |
| | Delete an idea through chat — confirmation card survives a page reload; Decline keeps the idea, Approve deletes it |
| `vault-flows` | Import a ChatGPT export: preview → commit as new ideas → current branch only, tool output excluded, proposals with provenance, re-import detected as duplicate |
| | Prompt injection in an imported conversation: flagged at import, never executed (no tool calls, no confirmation, idea count unchanged) |
| | Conclude an idea into an action plan, then an implementation prompt; download the Markdown through the UI |
| | Build a context pack to continue in ChatGPT; export it |
| `review-flows` | External AI delta: review the classified changes in the UI, merge a selection, verify the merge checkpoint |
| | Import Center: upload, preview, commit through the UI; instruction-like text flagged in the preview |
| `shell` | Phone width (390px): sidebar becomes a drawer, closes on navigation and Escape, no horizontal scroll |

Every test checks what the user sees **and** what was persisted (through the API with the same session).

Stable selectors: chat messages are `data-testid="message"` with `data-role` and `data-pending`; trace steps are `data-testid="trace-step"` with `data-tool` and `data-status`; the confirmation card is `data-testid="confirmation"`. Everything else is located by role and accessible name.

```bash
make e2e                                   # whole suite
cd apps/web && npx playwright test -g "fork"          # one flow
cd apps/web && npx playwright test --ui               # interactive
cd apps/web && npx playwright show-trace test-results/<test>/trace.zip   # after a failure
```

Port 5432 taken? Set `POSTGRES_PORT` in `.env`; the Makefile exports it to Playwright.

## Real-provider checks

Unit tests for providers use recorded HTTP responses, so they never need keys. To exercise a real provider end to end, put a key in `.env` (e.g. `GROQ_API_KEY`), run `make api`, and chat; or run `make eval-models`. The ChatGPT share-page adapter has an opt-in live test that reads a saved page you provide:

```bash
IDEAVAULT_REAL_SHARE_HTML=/path/to/saved-share.html IDEAVAULT_REAL_SHARE_MESSAGES=4 go test ./internal/imports/chatgpt/v2 -run TestRealShare -v
```

Real conversations are never committed; fixtures in `tests/fixtures` are synthetic.

## CI

`.github/workflows/ci.yml` runs only the jobs affected by a change (path filters), each with its own services:

- **api** – gofmt, go vet, staticcheck, `migrate verify`, `go test -race` against pgvector + Redis, OpenAPI drift check (the committed `packages/contracts/openapi.json` must match the routes), govulncheck.
- **web** – generated API types drift check, ESLint, `tsc`, Vitest, production build, `npm audit`.
- **e2e** – the Playwright suite against a production build.
- **secrets** – gitleaks over the full history.

Deploys (`deploy.yml`) run only after CI succeeds on `main`.
