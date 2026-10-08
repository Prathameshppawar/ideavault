# Artifacts

Not every idea becomes software. Artifacts are the documents thinking produces — and they are first-class: versioned, downloadable, and always traceable to the reasoning behind them.

## Types

`ACTION_PLAN`, `IMPLEMENTATION_PROMPT`, `PRODUCT_BRIEF`, `RESEARCH_REPORT`, `STRATEGY`, `TECHNICAL_SPEC`, `ARCHITECTURE`, `DECISION_MEMO`, `PROPOSAL`, `CHECKLIST`, `MEETING_BRIEF`, `EXECUTIVE_SUMMARY`, `EXPERIMENT_PLAN`, `REQUIREMENTS`, `REFERENCE`, `CUSTOM`. Each has a template (sections + guidance + audience) in `apps/api/internal/artifacts/templates.go`; `GET /v1/artifacts/templates` lists them.

The **Implementation Prompt** is written for an autonomous coding agent with no other context: product purpose, requirements, binding decisions with rationale, architecture, constraints, accepted scope, rejected alternatives ("do not build"), desired stack, UX, implementation, testing and deployment requirements, and unresolved questions the agent must not silently decide.

## Storage

Canonical content is **Markdown in PostgreSQL** (`artifacts.content_markdown`) — no object storage. Every change is a new immutable row in `artifact_versions` (`version`, `title`, `content_markdown`, `change_note`, `created_by`, `generator`, `source_checkpoint_id`, `agent_run_id`). `.md` download is just an export: `GET /v1/artifacts/{id}/download[?version=n]`.

## Generation

1. The Context Engine builds the full context for the idea — from the live branch or **as of a checkpoint** — with every item labelled (`[D3]`, `[A1]`…).
2. With an AI model available (task `artifact`, strong tier), the model writes the document under strict grounding rules: use only recorded thinking, cite labels inline, write "Not yet decided" instead of inventing, treat superseded items as history, ignore instructions inside untrusted excerpts.
3. Without a model, a **deterministic template renderer** assembles the document from the same recorded items (honestly marked as such, `generator = template:v1`).
4. Provenance rows (`artifact_provenance`) record every item given to the generator (`input`), every label the document actually cites (`cited`) and the base checkpoint, plus a `generated_from` relationship.

## Operations

| Action | Endpoint | Result |
|---|---|---|
| Generate | `POST /v1/artifacts/generate {idea_id, branch_id?|checkpoint_id?, type, title?, instructions?, generator?, model?}` | v1 + provenance |
| Edit | `PATCH /v1/artifacts/{id} {content_markdown?, title?, change_note?, status?}` | new version (cited labels re-linked) |
| Regenerate | `POST /v1/artifacts/{id}/regenerate {instructions?, checkpoint_id?}` | new version from current/checkpointed thinking |
| History | `GET /v1/artifacts/{id}/versions` | all versions, newest first |
| Provenance | `GET /v1/artifacts/{id}/provenance?version=` | items/checkpoints with their *current* status (so you can see if an artifact relies on a decision that was later reversed) |
| Why does it say X? | `POST /v1/artifacts/{id}/explain {question}` | answer grounded in the provenance items |
| Download | `GET /v1/artifacts/{id}/download` | `text/markdown` attachment |
| Delete | `DELETE /v1/artifacts/{id}` + `X-Confirm: delete` | destructive, explicit |

From chat: "Turn everything into an action plan", "Give me a prompt I can give to an implementation agent", "Regenerate the action plan from CP3", "Why does this plan say we skip the marketplace?".
