# Data model

PostgreSQL 17 with `pgvector`, `pg_trgm`, `citext`, `pgcrypto`. All ids are UUIDs, all user data is scoped by `user_id`, foreign keys and check constraints enforce integrity. Migrations live in [`db/migrations`](../db/migrations) (`NNNN_name.up.sql` / `.down.sql`), are checksummed, applied under an advisory lock, and verified reversible in CI (`make migrate-verify`).

## Entities

| Table | Purpose |
|---|---|
| `users`, `sessions`, `user_settings` | Owner account (Argon2id password), hashed session/API tokens, settings JSON (routing policy, agent policy). |
| `sources` | Provenance root for content: `ideavault_chat`, `import`, `url`, `paste`, `external_ai`, … with provider, adapter, content hash, `trusted` flag. |
| `conversations`, `messages` | Native chats and imported/external transcripts. `messages.untrusted = true` for anything not typed in IdeaVault. Full-text indexed. |
| `ideas` | Idea Space: `origin_text` (SOURCE, verbatim), `summary` (INTERPRETATION), lifecycle `status`, `outcome`, tags, default branch, merge target. |
| `idea_versions` | Immutable history of idea identity changes (every PATCH writes a version). |
| `branches` | Directions of thinking: parent branch, `forked_from_checkpoint_id`, `fork_mode` (checkpoint/selective/empty), head checkpoint. |
| `checkpoints` | **Immutable** snapshots (`snapshot` JSONB with full item copies, relationships, conversations, sources, artifacts, context) numbered per idea (CP1, CP2…), content hash. |
| `checkpoint_items` | Index of which item ids (and statuses) a checkpoint captured. Immutable. |
| `knowledge_items` | Shared table for all knowledge kinds: kind, `ref_number` (D3/A1/E2/I4/Q1/T2 labels), `lineage_id`, statement, details, status, `origin` (SOURCE / INTERPRETATION), `review_state` (PROPOSED / ACCEPTED / REJECTED), confidence, source excerpt/message/conversation/source, `inherited_from_id`, `superseded_by_id`. |
| `decisions`, `assumptions`, `evidence`, `insights`, `open_questions`, `action_items` | Kind-specific columns (rationale & alternatives; risk & validation method; stance, URL, strength, target; importance; answer; priority & due). Class-table inheritance keyed by `item_id`. |
| `relationships` | Typed edges between any two entities with origin, confidence, rationale, source message and agent run. |
| `branch_inheritance` | Exactly what a fork/merge inherited (`via` = checkpoint / selection / merge, source checkpoint & branch, new entity id). |
| `branch_links` | Conversations/sources/artifacts shared with a branch without copying. |
| `activity_events` | Append-only activity log powering timeline, momentum and journey. |
| `idea_conclusions` | Intentional conclusions: outcome, learned/decisions/unresolved summaries, final checkpoint, previous/new status. |
| `artifacts`, `artifact_versions`, `artifact_provenance` | Canonical Markdown in PostgreSQL; every edit/regeneration is a new immutable version; provenance rows tie each version to the items/checkpoints it used (`input`, `cited`, `base_checkpoint`). |
| `context_packs` | Portable Markdown + JSON snapshots of thinking for other AI tools. |
| `deltas`, `delta_items` | External AI conversation analysed against a branch: NEW / CHANGED / REJECTED / UNCHANGED, selection, merged item. |
| `analyses` | Stored prompt / thinking / contradiction / evolution analyses. |
| `agent_runs`, `tool_calls` | Supervisor runs with safe trace events (never chain-of-thought), resumable state for confirmations, each tool call with category, arguments, result summary, status, latency. |
| `model_configs` | Model registry (provider, model, capabilities, context, tool calling, structured output, vision, reasoning, relative cost, speed, quality, prices, enabled). |
| `usage_events` | Every model call: provider, model, operation, task, tokens (estimated flag), cost, latency, tool calls, conversation, idea, agent run. |
| `connectors`, `credentials`, `connector_events` | Connector state, AES-256-GCM encrypted secrets (key never stored in the DB), connector invocations. |
| `imports`, `import_items` | Import jobs and parsed conversations (preview, duplicate detection, injection flags, results). |
| `jobs` | PostgreSQL job queue (`FOR UPDATE SKIP LOCKED`, retries with backoff, dedupe keys). |
| `embeddings` | `vector(768)` per entity chunk and model, HNSW cosine index. |
| `model_lab_runs`, `model_lab_results` | Model Lab comparisons. |

## Invariants enforced by the database

- **Checkpoints, checkpoint items, idea versions, artifact versions and artifact provenance cannot be updated or deleted** (`iv_forbid_history_mutation` trigger). The only escape hatch is the transaction-local setting `ideavault.allow_history_delete = on`, used exclusively by user-confirmed deletion of a whole idea or artifact.
- **Knowledge content is immutable** (`iv_guard_knowledge_content` trigger): statement, details, kind, origin, excerpt, lineage, ref and ownership never change. Status, review state and `superseded_by` may change.
- Knowledge statuses are validated per kind by a check constraint.
- `(branch_id, kind, ref_number)` is unique; ref numbers are allocated per idea under an advisory lock, and forked copies keep their lineage's label — so `D3` means the same decision across branches.
- `MERGED` ideas must reference the idea they merged into.

## Branching semantics

Forking from a checkpoint **copies** the checkpoint's items into the new branch exactly as they were at that moment (a decision superseded later is still `ACTIVE` in the fork), with `inherited_from_id` and an `inherited_from` relationship to the original, and `branch_inheritance` rows recording what came from where. Selected later knowledge ("bring the research from CP7") is copied the same way with `via = selection`. Conversations, sources and artifacts are linked, not copied. Every new branch gets a `fork_base` checkpoint so its starting state is itself immutable.

## Search

Each searchable table has a generated `tsvector` column with a GIN index. Hybrid search = `websearch_to_tsquery` ranking + pgvector nearest neighbours under the active embedding model, merged by reciprocal-rank fusion; natural-language cues ("first", "never validated", "similar to X") become structured filters.
