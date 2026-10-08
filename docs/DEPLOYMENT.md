# Deployment

The frontend and backend deploy **independently**: web changes never redeploy the API and vice versa. Both deploy only after CI passes on `main` (`.github/workflows/deploy.yml`, path-aware).

| Part | Platform | Artifact | Cost |
|---|---|---|---|
| Web (`apps/web`) | Vercel (Hobby) | Next.js build | free |
| API (`apps/api`) | Railway | Docker image from `apps/api/Dockerfile` (built from repo root, includes `db/migrations`) | Railway's trial credit / Hobby plan — check your allowance |
| Database | Railway service `pgvector/pgvector:pg17` + volume (or Neon free tier with pgvector) | — | within the same allowance / free |
| Redis | optional (Railway Redis or Upstash free) — the API falls back to in-memory cache/rate limits | — | free / optional |
| AI | Groq / Gemini free tiers, or any provider you configure | — | usage-based |

No hosted queue, vector DB, object storage, auth or observability service is required.

## 1. API on Railway

One-time: install/login to the CLI (`npx @railway/cli login`). Then from the repo root:

```bash
WEB_ORIGIN=https://ideavault.vercel.app \
GROQ_API_KEY=gsk_… \
./scripts/deploy-railway.sh
```

The script (idempotent) creates the `ideavault` project, a `postgres` service from the pgvector image with a persistent volume, and the `ideavault-api` service with:

| Variable | Value |
|---|---|
| `APP_ENV` | `production` (enforces `MASTER_KEY`, secure cookies, HSTS) |
| `DATABASE_URL` | internal Railway URL to the pgvector service |
| `MASTER_KEY` | generated `openssl rand -base64 32` (kept in gitignored `.railway-deploy.env` so re-runs reuse it) |
| `WEB_ORIGINS` | your Vercel origin |
| `COOKIE_SECURE` | `true` |
| `GROQ_API_KEY` / other provider keys | passed via stdin (never echoed) |

It then runs `railway up` (Dockerfile build per `railway.toml`, health check `/readyz`) and generates a public `*.up.railway.app` domain. Migrations run automatically at startup (`AUTO_MIGRATE=true`, advisory-locked, checksum-verified).

**Alternative database:** Neon free tier supports pgvector — set `DATABASE_URL` to the Neon connection string (`sslmode=require`) and skip the postgres service.

## 2. Web on Vercel

```bash
API_ORIGIN=https://ideavault-api-production.up.railway.app ./scripts/deploy-vercel.sh
```

This links `apps/web` to a Vercel project named `ideavault` (→ `https://ideavault.vercel.app` if available), sets the **server-side** `API_ORIGIN` (used by the `/api/*` rewrite; never exposed to the browser) and deploys to production. `apps/web/vercel.json` skips builds when nothing under `apps/web` or `packages/contracts` changed.

Because the browser only talks to the Vercel origin, the session cookie is first-party; make sure `WEB_ORIGINS` on the API equals the Vercel URL.

## 3. Continuous deployment (GitHub Actions)

Add repository secrets:

| Secret | Where to get it |
|---|---|
| `RAILWAY_TOKEN` | Railway → project → Settings → Tokens (project token) |
| `VERCEL_TOKEN` | Vercel → Account Settings → Tokens |
| `VERCEL_ORG_ID`, `VERCEL_PROJECT_ID` | `apps/web/.vercel/project.json` after the first `vercel link` |

Optional repository variable `RAILWAY_SERVICE` (default `ideavault-api`). Without the secrets, the deploy jobs are skipped with a warning (CI still runs).

Flow: push to `main` → **CI** (lint, unit + integration tests with Postgres/pgvector, migration up/down/up, contract drift check, security scans, web build, Playwright E2E) → on success **Deploy** computes which paths changed → deploys only the API and/or only the web app.

## 4. First run in production

Open the web URL → you'll be sent to **/setup** to create the owner account (only possible while the vault is empty). Alternatively set `BOOTSTRAP_EMAIL` / `BOOTSTRAP_PASSWORD` on the API once.

## Operations

- Health: `GET /healthz` (process), `GET /readyz` (Postgres, Redis, AI configuration).
- Logs: structured JSON with `request_id`, `agent_run_id`, `tool_call_id`, `user_id`; secrets redacted.
- Backups: snapshot the Railway volume or use `pg_dump` (`DATABASE_URL` from the Railway dashboard).
- Rotating `MASTER_KEY` makes stored provider/connector credentials unreadable — re-enter them afterwards.
- Scaling: run **one API instance** (the default on Railway). Jobs (`SKIP LOCKED`) and migrations (advisory lock) are already safe across instances, but the guard that stops two chat turns running at once in the same conversation is in-process. Before scaling out, move that guard to Redis or a PostgreSQL advisory lock, and set `REDIS_URL` so rate limits are shared.
