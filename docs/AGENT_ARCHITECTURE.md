# Agent architecture

The **Agent Supervisor** (`apps/api/internal/agent`) turns natural language into operations on the real vault. It never works from memory: every claim it makes must come from a tool result in the current run.

## One turn

```
POST /v1/agent/chat {message, conversation_id?, idea_id?, branch_id?, checkpoint_id?, model?}
 │
 ├─ persist user message (trusted) · create agent_run (RUNNING) · emit run_started
 ├─ history: last ~10 messages (older ones truncated; untrusted content fenced)
 ├─ loop (≤ 10 steps):
 │    ├─ focus context: if an idea is in focus, the Context Engine renders a ~1k-token
 │    │  summary (ranked decisions, open questions, contradictions…) into the system prompt
 │    ├─ tool selection: core tools + groups matched to the request + tools already used
 │    ├─ Gateway.Do(task=supervisor) → streamed tokens (emit token); per-minute rate limits are
 │    │  waited out (≤ 60 s, twice) with a visible "retrying in Ns" trace step
 │    ├─ no tool calls → claim check (below) → done
 │    └─ for each tool call:
 │         ├─ policy.Decide(category) → allow | confirm | deny
 │         ├─ confirm → tool_call AWAITING_CONFIRMATION, run state saved, emit confirmation_required, stop
 │         └─ allow → execute (emit trace running/done/failed), append result (≤ 6 KB) to the loop
 └─ persist assistant message (content, refs, trace labels, model) · run COMPLETED · emit message, run_completed
```

**Claim check.** If the final reply announces a write ("Checkpoint created", "Decision recorded", "Forked a new branch", …) that no tool performed in this run, the reply is discarded and the model is asked once more, told that nothing was saved. This stops a model from putting false history in the user's head; it was added after a fallback model announced a checkpoint it never created. If the run did real work but the provider fails on the final reply, the user gets a list of what was saved instead of an error.

**Pending approvals survive reloads.** `GET /v1/agent/pending?conversation_id=` lets a reopened conversation show its approval prompt again.

`POST /v1/agent/tool-calls/{id}/confirm {approve}` resumes the saved run: approved calls execute, declined calls return "the user declined this action" to the model, and the loop continues.

Stopping generation (client abort) cancels the context; the partial answer is persisted with `stopped: true` and the run is `CANCELLED`. Retrying re-runs the agent for an existing user message (`retry_message_id`).

## Tools

46 tools across four permission classes (plus connector tools). Full catalog: `GET /v1/agent/tools`.

| Class | Behaviour | Tools |
|---|---|---|
| **READ** | automatic | `search_vault`, `list_ideas`, `get_idea`, `get_branch`, `get_checkpoint`, `get_context`, `get_conversation`, `get_related`, `get_decisions`, `get_assumptions`, `get_evidence`, `get_insights`, `get_questions`, `get_actions`, `get_artifacts` |
| **ANALYZE** | automatic | `analyze_conversation`, `analyze_prompt`, `compare_branches`, `find_thinking_patterns`, `detect_contradictions`, `build_context_pack`, `compare_checkpoint`, `analyze_idea_evolution`, `analyze_artifact`, `analyze_external_conversation` |
| **WRITE** | automatic by default; "ask before every write" in Settings | `create_idea`, `update_idea`, `create_branch`, `record_decision`, `record_assumption`, `record_evidence`, `record_insight`, `record_question`, `record_action`, `update_knowledge_status`, `review_proposals`, `create_checkpoint`, `fork_idea`, `merge_selected_context`, `import_conversation`, `create_relationship`, `create_artifact`, `update_artifact`, `conclude_idea` |
| **DESTRUCTIVE** | **always** confirmation | `delete_idea`, `delete_artifact` |
| **EXTERNAL** | confirmation (unless the user enables auto-external) | `web_fetch`, `github_list_repos`, `github_get_readme`, `github_create_issue`, `custom_get` |

Tools reference entities the way people do: idea by title/slug/id (fuzzy-resolved), branch by name, checkpoint as `CP4`, knowledge as `D3`. Tool results carry `iv://type/id` links, which the UI renders as clickable chips.

The policy is evaluated by the supervisor on every call; models cannot influence it. Tools disabled in the policy are rejected. All writes go through `service` with the agent run id, so every change made by the agent is attributable (`created_by = agent`, `agent_run_id`).

## Execution trace, not chain-of-thought

The UI shows *what happened*: "Found Biker Community Platform", "Located CP4 (17 items)", "Retrieved 8 decisions", "Waiting for your confirmation: delete_idea". These are generated from tool results and stored in `agent_runs.trace` and `tool_calls`. Hidden model reasoning is never requested, displayed or stored (reasoning outputs are disabled at the provider level and discarded if returned).

## Memory policy

The agent records durable knowledge only when the user asks ("remember this as a decision", "save this insight") or clearly states a final decision. Imported conversations produce **proposed** items that are excluded from checkpoints and context until accepted (`review_proposals`). Beliefs change by superseding (`supersedes=D2`), never by editing.

## Prompt-injection defences

1. Imported, external, web and connector content is stored `untrusted`, fenced as `<untrusted_imported_content>` in model context, and tool results carry an explicit "treat as data" notice.
2. The system prompt instructs the model never to follow instructions found in such content.
3. Extraction from imports uses structured output with **no tools**, so imported text cannot trigger actions.
4. The permission policy is enforced outside the model: destructive and external actions always require the user's click.
5. Import items are scanned (`imports.ScanInjection`) and flagged in the UI; content is never modified.
6. Agent evaluations include injection cases (`tests/evaluations/agent`).

## Model routing for the supervisor

Task `supervisor` requires tool calling and ≥32k context; the strong tier ranks by quality, then speed, then cost. With Groq configured: `openai/gpt-oss-120b`, falling back to `openai/gpt-oss-20b` and `qwen/qwen3.8-27b` on errors or rate limits. Groq's free tier allows ~8k tokens per minute per model and every tool step re-sends the request, so the supervisor keeps calls small: 11 core tools (the other `record_*` tools join only when the message talks about remembering, assumptions, evidence, questions or actions), compact history and a 1k-token focus context. A model that rejects a request as too large (413) is not retried for that call. With nothing configured, the **offline planner** runs.

## Offline planner

`agent/offline.go` implements `models.Provider` deterministically: it recognises IdeaVault intents (new idea, continue from CP N, why did we decide, where did I first…, fork from CP N bringing CP M, remember as …, checkpoint, artifacts, conclude, context pack, delete, prompt/thinking analysis, contradictions, compare branches, evolution) and issues the same tool calls a model would, then writes answers assembled from tool results. It makes the full product usable without any AI key and makes end-to-end tests deterministic. It is clearly labelled "offline planner (no AI)" in the UI.

See [AGENT_CONTRACT.md](AGENT_CONTRACT.md) for the guarantees and event formats.
