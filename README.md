# IdeaVault

**A personal AI thinking system.** IdeaVault remembers how an idea came to exist, how it evolved, what was considered, why decisions were made, what branches you explored — and turns that thinking into preserved, reusable outcomes: action plans, implementation prompts, specs, research reports, decision memos and more.

It is not a notes app. The database is a persistent graph of your thinking; the chat is how you operate it; the dashboard is how you see it.

```
raw thought → exploration → conversation → research → assumptions → evidence
            → insights → decisions → branches → checkpoints → outcome
```

## What you can do

Talk to it:

- *"I have a new idea for a biker community platform where people can create trips."* → an Idea Space with a Main branch, your words preserved as its origin, duplicates checked first.
- *"I've got a new idea. This is the conversation I had about it: https://chatgpt.com/share/…"* → the conversation is imported (untrusted, with provenance) and knowledge is proposed for your review.
- *"Remember this as a decision: no marketplace in v1, because moderation would delay launch."*
- *"Create a checkpoint."* → an immutable snapshot (CP1, CP2…).
- *"Continue the biker platform from checkpoint 4."*
- *"Why did we decide not to build the marketplace?"* → rationale, rejected alternatives, evidence, the original source excerpt, and how the decision evolved.
- *"Find where I first talked about AI automation."*
- *"Fork this idea from checkpoint 4 but bring the research from checkpoint 7."*
- *"I think we're done with this idea. Turn everything into an action plan."* / *"Give me a prompt I can give to an implementation agent."*

And see it: Today (open loops, recent decisions and insights), Universe (interactive graph), Timeline, Active, Decisions (with reversals), Learning, Momentum, Archive; idea pages with Journey, Knowledge, branch tree, checkpoints, conversations and artifacts; Import Center; Model Lab; usage analytics; connectors.

## Stack

| | |
|---|---|
| Backend | Go 1.26 · chi · pgx · PostgreSQL 17 + pgvector · optional Redis · `apps/api` |
| Frontend | Next.js 16 (App Router, Cache Components) · React 19 · TypeScript · Tailwind v4 · TanStack Query · `apps/web` |
| AI | Provider-agnostic gateway: Groq, OpenAI, Anthropic (official SDK), Gemini, Ollama/local, deterministic offline planner |
| Contracts | OpenAPI generated from the Go routes → `packages/contracts/openapi.json` → TS types |
| Tests | Go unit + integration (real Postgres), API tests, agent evaluations, Vitest + Testing Library, Playwright E2E |
| Delivery | Docker, GitHub Actions (path-aware CI), Railway (API), Vercel (web) |

```
ideavault/
├── apps/api/          Go API (cmd/{api,migrate,openapi,eval}, internal/…)
├── apps/web/          Next.js app
├── packages/contracts OpenAPI document (generated)
├── db/migrations/     SQL migrations (up/down, verified reversible)
├── docs/              Architecture, data model, agent, API, imports, security, deployment…
├── tests/             fixtures (imports, seed), evaluation notes (E2E: apps/web/tests/e2e)
├── infra/             local Postgres init
└── .github/workflows/ CI + deploy
```

## Run it locally

Prerequisites: Docker, Go 1.26+ (older Go downloads the 1.26 toolchain automatically), Node 22+.

```bash
git clone https://github.com/Prathameshppawar/ideavault.git && cd ideavault
cp .env.example .env            # optional: add GROQ_API_KEY (or any provider) for real AI
make setup                      # Go modules, npm packages, Playwright browser
make up                         # PostgreSQL (pgvector) + Redis via docker compose
make api                        # API on http://localhost:8080  (migrates automatically)
make web                        # web on http://localhost:3000   (in another terminal)
```

Open http://localhost:3000, create your vault (the first account becomes the owner), and start chatting. Optional demo data: `make seed`.

> Port 5432 busy? Set `POSTGRES_PORT=5442` in `.env` (and use it in `DATABASE_URL`).

**AI providers are optional.** Without any key IdeaVault runs in *offline mode*: a deterministic planner operates the vault through the same tools and is clearly labelled "offline planner (no AI)". Add keys in `.env` or in the UI (Models → Providers; stored encrypted). See [.env.example](.env.example) for every setting.

> **Groq's free tier** allows about 8k tokens per minute per model. IdeaVault keeps requests small and visibly waits out per-minute limits ("AI provider rate limit — retrying in Ns"), so busy turns can take 30–50 s. Adding a second provider (e.g. a free Gemini key) spreads the load.

## Tests

```bash
make test          # Go unit + integration (needs `make up`) + web unit tests
make e2e           # Playwright: real API (offline planner) + web, isolated database
make eval          # agent evaluation cases
make lint          # gofmt, go vet, eslint, tsc
make migrate-verify
```

## Deploy

Frontend → Vercel, backend → Railway (Docker), deployed independently by GitHub Actions after CI passes. Step-by-step: [docs/DEPLOYMENT.md](docs/DEPLOYMENT.md).

## Documentation

[Architecture](docs/ARCHITECTURE.md) · [Data model](docs/DATA_MODEL.md) · [Agent architecture](docs/AGENT_ARCHITECTURE.md) · [Agent contract](docs/AGENT_CONTRACT.md) · [API](docs/API.md) · [Imports](docs/IMPORTS.md) · [Connectors](docs/CONNECTORS.md) · [Artifacts](docs/ARTIFACTS.md) · [Testing](docs/TESTING.md) · [Deployment](docs/DEPLOYMENT.md) · [Security](docs/SECURITY.md) · [Decisions](docs/DECISIONS.md) · [UI guide](apps/web/UI_GUIDE.md)

## Principles

Local-first · provenance everywhere · history is never rewritten · source and interpretation stay distinguishable · provider-agnostic · imported content is never trusted · no chain-of-thought exposure · no fake functionality.
