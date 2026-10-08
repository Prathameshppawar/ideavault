# Agent contract

What the Agent Supervisor guarantees, and the wire format clients rely on.

## Guarantees

1. **Grounded.** Statements about the user's ideas come from tool results in the same run. If nothing is found, the agent says so.
2. **No rewriting history.** Checkpoints, artifact versions and idea versions are immutable (database-enforced). Knowledge changes by supersession; the old item stays retrievable with `superseded_by`.
3. **Permissions are not negotiable.** READ/ANALYZE run automatically; WRITE runs automatically unless the user's policy asks for confirmation; DESTRUCTIVE always pauses for the user; EXTERNAL pauses unless explicitly allowed. A paused run resumes only via `POST /v1/agent/tool-calls/{id}/confirm`.
4. **Untrusted data stays data.** Imported/external/web/connector content can inform answers but never directs actions.
5. **Provenance.** Every agent write records `created_by = agent` and the `agent_run_id`; knowledge records its origin (SOURCE with verbatim excerpt / source message, or INTERPRETATION) and artifacts record exactly which items and checkpoint produced each version.
6. **No chain-of-thought.** Only safe execution events are exposed and stored.
7. **Memory policy.** Not every sentence becomes knowledge; explicit commands or clear decisions do. Imports produce proposals.

## SSE events (`POST /v1/agent/chat`, `POST /v1/agent/tool-calls/{id}/confirm`)

Each event is `event: <type>\ndata: <json>\n\n`; `: keep-alive` comments are sent every 15 s.

| Event | Data |
|---|---|
| `run_started` | `{run_id, conversation_id, user_message_id?, conversation_title?, resumed?, approved?}` |
| `token` | `{text}` — visible answer text, streamed |
| `trace` | `{at, kind: step|tool|model|permission, label, tool?, tool_call_id?, status: running|done|failed|awaiting_confirmation|approved|denied|waiting, category?, refs?}` |
| `confirmation_required` | `{tool_call_id, tool, category, arguments, description, run_id}` — the stream then ends; the run is `AWAITING_CONFIRMATION` |
| `message` | `{id, role: "assistant", content, refs[], trace[], model, provider, created_at}` — the persisted assistant message |
| `run_completed` | `{run_id, status: COMPLETED|FAILED|CANCELLED, usage{input_tokens, output_tokens}, model, provider, focus{idea_id, branch_id, checkpoint_id}}` |
| `error` | `{message}` — user-presentable (secrets redacted) |
| `done` | `{}` — always last |

## Entity links

Assistant Markdown uses `iv://<type>/<uuid>` links (`idea`, `branch`, `checkpoint`, `decision`, `assumption`, `evidence`, `insight`, `question`, `action`, `artifact`, `conversation`, `context_pack`, `delta`). Clients render them as navigable chips; they are never fetched as URLs.

## Request fields

| Field | Meaning |
|---|---|
| `message` | The user's text (≤ 200k chars). |
| `conversation_id` | Continue a native conversation (imported ones are read-only). |
| `idea_id`, `branch_id`, `checkpoint_id` | Initial focus (e.g. chat panel on an idea page, or "viewing CP4"). |
| `model` | Optional `provider/model` pin for this run (no fallback when pinned). |
| `retry_message_id` | Re-run the agent for an existing user message without duplicating it. |

## Evaluation

`tests/evaluations/agent/cases.json` encodes expected behaviour (tool sequences, forbidden tools, permission respect, injection resistance, provenance) and runs against the offline planner in CI (`make eval`) and against any configured model with `EVAL_MODEL=provider/model go run ./cmd/eval agent`.
