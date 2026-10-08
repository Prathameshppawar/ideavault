# Architectural decisions

Short ADR-style records of choices made while building IdeaVault, with the reasoning and the alternatives rejected.

### 1. One Go service + PostgreSQL, no microservices
**Decision:** a single Go binary serves the API, runs the job worker and talks to PostgreSQL (and optionally Redis).
**Why:** personal scale, ₹0 infrastructure target, fewer moving parts to secure and deploy. **Rejected:** separate worker service, hosted queue, hosted vector DB.

### 2. PostgreSQL is the system of record — including the job queue and vectors
**Decision:** jobs use `SELECT … FOR UPDATE SKIP LOCKED`; embeddings use pgvector with HNSW.
**Why:** transactional consistency with the data the jobs touch; one backup. Redis is only a cache/rate-limit accelerator with in-memory fallbacks.

### 3. Knowledge as class-table inheritance
**Decision:** one `knowledge_items` table (shared columns, lineage, provenance, review state) + per-kind tables (`decisions`, `assumptions`, `evidence`, `insights`, `open_questions`, `action_items`).
**Why:** checkpoints, forks, search, graph and contradiction detection treat knowledge uniformly, while each kind keeps typed, constrained columns. **Rejected:** six unrelated tables (every generic operation becomes a six-way union), one JSON blob (no constraints).

### 4. Immutability enforced by triggers
**Decision:** checkpoints, checkpoint items, idea versions, artifact versions and provenance reject UPDATE/DELETE in the database; knowledge content columns are guarded. Deleting a whole idea uses a transaction-local escape hatch.
**Why:** "never rewrite history" must survive bugs and prompt injection.

### 5. Forks copy, with lineage
**Decision:** forking copies the checkpoint's items *as they were* into the new branch, keeping their human label (D3) and `lineage_id`; `branch_inheritance` records exactly what came from where; conversations/artifacts are linked, not copied.
**Why:** branches are fully independent afterwards (no read-time overlay logic), comparisons work by lineage, and provenance is explicit. Selected later knowledge is copied the same way with `via = selection`.

### 6. Checkpoint numbers per idea
**Decision:** CP numbers are unique per idea across all branches.
**Why:** "fork from checkpoint 4 but bring the research from checkpoint 7" must be unambiguous even when CP7 lives on another branch.

### 7. Proposed vs accepted knowledge
**Decision:** extraction (imports, external deltas, re-extraction) writes `PROPOSED` items that don't count until reviewed; the agent records durable knowledge only on explicit request or clear decisions.
**Why:** the spec's memory policy — not every sentence is knowledge — and imported text is untrusted.

### 8. Provider-agnostic gateway with task routing
**Decision:** every model call declares a *task*; the router filters models by capability and ranks by tier (fast/strong), quality, speed, cost and user pins; failures fall back to the next candidate; usage and estimated cost are recorded per call.
**Why:** no provider is always best; free tiers rate-limit; the user should see and control cost.

### 9. Groq as the default provider in this deployment
**Decision:** with the provided key, Groq's `openai/gpt-oss-120b` (strong) and `openai/gpt-oss-20b` (fast) serve all tasks (verified tool calling + JSON-schema output); Llama 3.x models were removed from the seed catalog because the account no longer serves them.
**Consequence:** Groq has no embeddings API, so embeddings use the local hashing embedder unless an OpenAI/Gemini/Ollama embedder is configured. The free tier's ~8k tokens/minute per model is handled by a compact tool subset per turn, history compaction and waiting out per-minute limits (≤ 60 s, up to twice, shown in the trace). Busy turns can take 30–50 s; adding a second provider (e.g. Gemini's free tier) spreads the load.

### 10. Deterministic offline planner instead of fake AI
**Decision:** when no model is configured (and in tests) a rule-based planner issues real tool calls and writes answers assembled from tool results, labelled "offline planner (no AI)". Artifacts fall back to a deterministic template renderer; extraction to conservative phrasing heuristics.
**Why:** the product stays functional and honest without keys, and E2E tests are deterministic. **Rejected:** canned/fake AI responses.

### 11. Intent-based tool selection
**Decision:** the supervisor offers core tools plus groups matched to the request (and tools already used in the run) instead of all ~46.
**Why:** ~6k tokens of tool schemas per call exceeded free-tier budgets; fewer tools also improves tool choice. Execution of any registered tool is unaffected.

### 12. Same-origin API via Next.js rewrites
**Decision:** the browser calls `/api/*` on the web origin; Next.js proxies to the Go API.
**Why:** first-party `HttpOnly` cookies (third-party cookies are blocked by browsers), simple CSRF model, the API URL is never exposed to the browser.

### 13. Cookie sessions + custom-header CSRF, bearer tokens for scripts
**Why:** simplest secure model for a personal app; no JWT revocation problem; tokens are revocable rows.

### 14. Generated OpenAPI from the Go route registry
**Decision:** routes are registered with request/response types; the OpenAPI document is built by reflection and checked in CI; TypeScript types are generated from it.
**Why:** documentation and frontend types cannot drift from the implementation.

### 15. Markdown as the canonical artifact format
**Why:** portable, diffable, renders everywhere, trivially downloadable; stored in PostgreSQL so no object storage is needed.

### 16. Hybrid search with reciprocal-rank fusion
**Why:** full-text is exact and explainable; embeddings catch paraphrase. Natural-language cues ("first", "never validated", "similar to") are mapped to structured filters and shown back to the user as the interpretation.

### 17. Next.js 16 with Cache Components, client-rendered screens
**Decision:** route files are tiny server components that render client screens under `<Suspense>`; data is fetched with TanStack Query through the same-origin API.
**Why:** the app is behind auth and highly interactive (streaming chat, graphs); this keeps navigation fast while satisfying Cache Components' rules for runtime params.

### 18. Verify claimed actions before replying
**Decision:** a final reply that announces a write (checkpoint, decision, idea, branch, artifact) without a matching successful tool call in the run is discarded and the model is asked once more.
**Why:** in a live run a fallback model replied "Checkpoint created — CP5" without calling the tool. For a system whose value is trustworthy history, an invented action is worse than an error. **Rejected:** forcing `tool_choice=required` (breaks legitimate answers), trusting the model.

### 19. One API instance
**Decision:** deploy a single API instance. Jobs (`SKIP LOCKED`) and migrations (advisory lock) are multi-instance safe; the per-conversation chat lock is in-process.
**Why:** personal use at ₹0. Before scaling out, move that lock to Redis or a PostgreSQL advisory lock.

