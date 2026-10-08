# Architecture

IdeaVault is a personal AI thinking system. Its job is to remember **how an idea came to exist, how it evolved, what was considered, why decisions were made and what outcomes it produced** — and to let you operate all of that through a chat.

```
            ┌──────────────────────────── Browser ────────────────────────────┐
            │  Next.js 16 app (apps/web)                                       │
            │  chat · idea pages · journey · universe graph · artifacts · …    │
            └───────────────┬──────────────────────────────────────────────────┘
                            │ same-origin  /api/*  (rewrite → API_ORIGIN)
                            ▼
┌──────────────────────── Go API (apps/api) ─────────────────────────────────────┐
│ handler/http   REST /v1, SSE chat stream, OpenAPI (generated), auth, CSRF,     │
│                rate limits, security headers, request IDs                       │
│ agent          Supervisor: tool loop · permission policy · safe trace ·         │
│                offline planner (deterministic fallback)                         │
│ service        business rules shared by HTTP and agent tools                    │
│ contextengine  smallest useful context for a request (ranked, budgeted)         │
│ artifacts      templates + grounded generation + provenance                     │
│ imports        versioned adapters (chatgpt/v1, chatgpt/v2, claude/v1, …)        │
│ models         provider abstraction · registry · task router · gateway          │
│                (fallback, retries, usage & cost accounting) · embedders         │
│ connectors     registry (web, github, custom, OAuth placeholders, exports)      │
│ jobs           PostgreSQL job queue worker (imports, embeddings)                 │
│ repository     postgres (pgx) · redis (cache/rate limit, optional)               │
└───────────────┬──────────────────────────────────┬─────────────────────────────┘
                ▼                                  ▼
     PostgreSQL 17 + pgvector              Redis (optional accelerator)
     (system of record)                    cache · rate limiting
```

## Core idea: the database is a cognitive graph

Everything the user thinks lands in one graph (see [DATA_MODEL.md](DATA_MODEL.md)):

- **Idea Space** → the persistent identity of an idea (title, *origin text* = what the user said, *summary* = IdeaVault's interpretation, lifecycle status).
- **Branches** → directions of thinking. The original branch is never overwritten.
- **Checkpoints** → immutable snapshots of a branch's complete knowledge state (enforced by database triggers).
- **Knowledge** → decisions, assumptions, evidence, insights, questions, actions. Content is append-only: beliefs change by *superseding*, never by editing.
- **Relationships** → typed, provenance-carrying edges (supports, contradicts, supersedes, inherited_from, similar_to, …).
- **Conversations / messages / sources** → where everything came from. Imported and external content is flagged *untrusted*.
- **Artifacts** (versioned Markdown) and **context packs** → outcomes, always linked back to the thinking that produced them.

The chatbot is the primary interface; the dashboard is a visualisation of the same graph.

## Request paths

**Chat (agent):** `POST /v1/agent/chat` (SSE) → Supervisor persists the user message, builds a compact focus context, offers a task-relevant subset of tools to the routed model, executes tool calls through the permission policy (DESTRUCTIVE/EXTERNAL pause for confirmation), streams tokens and safe trace events, persists the assistant message with entity references. See [AGENT_ARCHITECTURE.md](AGENT_ARCHITECTURE.md).

**UI:** every screen calls REST endpoints that go through the same `service` layer the agent uses, so the UI and the chatbot can never disagree about the rules.

**Imports:** upload/paste/URL → `imports` row (raw payload) → job `import.parse` → adapters → `import_items` (preview, dedupe, injection flags) → user commits → job `import.commit` → sources, conversations, untrusted messages, optional extraction into **proposed** knowledge. See [IMPORTS.md](IMPORTS.md).

**Embeddings:** writes enqueue `embed.entity` jobs; vectors (768-d) live in `embeddings` with an HNSW index; search fuses PostgreSQL full-text and vector results with reciprocal-rank fusion.

## Model layer

`models.Gateway` is the only way code talks to an LLM:

```
task (e.g. "extraction") → required capabilities (tools / JSON / context) → available models
  → routing policy (tier: fast|strong, quality/speed/cost, user pins & preferred providers)
  → ranked candidates → call → fallback to next candidate on failure → usage + cost recorded
```

Providers: OpenAI-compatible (OpenAI, Groq, Ollama/LM Studio), Anthropic (official Go SDK), Google Gemini (REST), and the deterministic **offline planner** (`mock`), which is used only when nothing else is configured or in tests. Reasoning models are configured so that chain-of-thought is never returned (Groq `include_reasoning:false` / `reasoning_format:hidden`, Anthropic thinking blocks only replayed opaquely). Per-minute rate limits are waited out (the provider's suggested delay, ≤ 60 s, at most twice); daily quotas and oversized requests fail fast.

## Why this shape

- **One Go binary + PostgreSQL** keeps infrastructure at ₹0 for personal use: no separate queue, vector DB, or auth service. Redis is optional.
- **Services shared by HTTP and agent** guarantee the chatbot can do what the UI does (and is bound by the same validation).
- **History in the database, enforced by triggers** — not by convention — so no bug or prompt can rewrite a checkpoint.
- **Provider-agnostic routing** — no single model is assumed best; tasks pick models by capability.

See [DECISIONS.md](DECISIONS.md) for the reasoning behind specific choices.
