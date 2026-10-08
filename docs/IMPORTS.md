# Imports

The Import Center brings conversations from other AI tools into IdeaVault with full provenance. **Imported content is untrusted data**: it is stored, searched and analysed, but never treated as instructions.

## Supported inputs

| Adapter | Input | Notes |
|---|---|---|
| `chatgpt/v1` | Official ChatGPT data export `conversations.json` (or the export `.zip`) | Reconstructs the active thread from `current_node`; skips system/tool/hidden/"thoughts" nodes and tool invocations; resolves citation markers (U+E200…U+E201) into Markdown links. |
| `chatgpt/v2` | Public share page `https://chatgpt.com/share/<id>` (HTML) | Decodes the React Router turbo-stream payload embedded in the page and reuses the v1 message logic. |
| `claude/v1` | Claude export `conversations.json` (or `.zip`) | Uses text content blocks; never imports thinking/tool blocks. |
| `gemini/v1` | Google Takeout → My Activity → Gemini Apps `MyActivity.json` | Groups prompts/responses into conversations by time gaps (Takeout has no conversation ids). |
| `markdown/v1`, `text/v1` | Pasted transcripts (`You said:` / `ChatGPT said:`, `User:` / `Assistant:`, `## User`, …) or notes | Two or more user+assistant markers ⇒ conversation; otherwise stored as a single document. |
| `json/v1` | Generic `{title, messages:[{role, content}]}`, OpenAI-style message arrays | Low detection priority so platform adapters win. |
| `web/v1` | Any public web page | Readable text extraction (drops nav/footers/scripts). |

Adapters are versioned (`internal/imports/<provider>/vN`) so new export formats get new adapters without breaking old ones. Detection picks the highest-confidence adapter; you can force one.

## What IdeaVault will not do

- It never asks for your ChatGPT/Claude/Gemini password.
- It cannot open **private** conversation links (`chatgpt.com/c/…`, `claude.ai/chat/…`, `gemini.google.com/app/…`) and says so, pointing you to *Share → copy link* or the official export.
- Claude and Gemini share pages are client-rendered; if no conversation data is embedded, IdeaVault reports that and recommends the export instead of guessing.

## Flow

1. **Create** — upload (multipart), paste or URL → `imports` row (raw payload kept only until parsed) → job `import.parse`. Large files never block the request.
2. **Parse & preview** — adapters produce normalized conversations → `import_items` with message count, preview excerpts, content hash, **duplicate detection** (same provider+external id or same content hash) and **prompt-injection flags** (`ignore_instructions`, `role_override`, `system_prompt_probe`, `destructive_command`, `tool_call_markup`, `chat_template_tokens`, `exfiltration`). Status `PREVIEW`. The raw payload is deleted.
3. **Commit** — choose items and destinations (new idea / existing idea / store only) and whether to extract knowledge → job `import.commit`.
4. **Persist** — per item: a `source` (provider, adapter, URI, hash, untrusted), a `conversation` (origin `imported`), `messages` (untrusted), optional new Idea Space whose *origin text* is the first user message, and optional **extraction** into **proposed** knowledge items with `source_message_id` provenance and verbatim excerpts.
5. **Status** — `QUEUED → PROCESSING → PREVIEW → PROCESSING → COMPLETED | PARTIAL | FAILED`, with per-item errors; failures of one conversation don't fail the import.

Small inputs can be imported synchronously (`sync=true`) — that's what the chat uses for "here's the conversation I had about it: <share link>".

## Safety limits

- Upload cap `MAX_UPLOAD_MB` (default 200 MB); ZIPs are read in memory with per-entry and total decompression limits and rejected if they contain unsafe paths.
- URL fetching uses an SSRF-safe client: http(s) only, ports 80/443, every resolved IP must be public (checked at dial time, defeating DNS rebinding), bounded redirects, size and time limits, no proxy.
- NUL bytes and invalid UTF-8 are stripped; very large messages are truncated with a warning.

## Testing

Synthetic fixtures for every adapter live in [`tests/fixtures/imports`](../tests/fixtures/imports) (including a generated share page and an injection sample). Adapter tests run with `go test ./internal/imports/...`; end-to-end import flows run in the integration and Playwright suites.
