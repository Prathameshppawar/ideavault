# Security

IdeaVault holds private thinking. The design assumes the browser, imported content and AI models are all potential sources of hostile input.

## Threats and controls

| Threat | Controls |
|---|---|
| **SQL injection** | All queries are parameterised (pgx); dynamic filters use a positional-argument builder, never string interpolation of values. |
| **XSS** | React escapes by default; Markdown (chat, artifacts, imports) is rendered with `react-markdown` without raw HTML, a URL allow-list (`http(s)`, `mailto`, relative, `iv://`), `javascript:` links dropped, remote images limited to https with `no-referrer`. API responses send `Content-Security-Policy: default-src 'none'`. Web pages send `nosniff`, `X-Frame-Options: DENY`, `Referrer-Policy`, `Permissions-Policy`. |
| **CSRF** | Session cookie is `HttpOnly; SameSite=Lax` (`Secure` in production). Cookie-authenticated unsafe requests must carry `X-IdeaVault-CSRF: 1` and, if an `Origin` is present, it must be an allowed origin. CORS only grants configured `WEB_ORIGINS`. Bearer-token requests are exempt (not ambient credentials). |
| **Session theft / weak auth** | Argon2id password hashing (64 MiB, t=2), constant-time comparison, equalised timing for unknown users, 256-bit random tokens stored only as SHA-256 hashes, revocable sessions and API tokens, auth endpoints rate-limited. First-run setup is only possible while the vault has no owner. |
| **Unauthorized data access** | Every repository query is scoped by `user_id`; relationship endpoints verify ownership of both ends; cross-idea references (branch/checkpoint/item belonging to another idea) are rejected. |
| **Prompt injection** | Imported/external/web/connector content is stored `untrusted`, fenced in model context, labelled in tool results, scanned and flagged; extraction runs without tools; DESTRUCTIVE and EXTERNAL tools always need a human click enforced outside the model; evaluation cases cover injection. See [AGENT_ARCHITECTURE.md](AGENT_ARCHITECTURE.md#prompt-injection-defences). |
| **Connector permission bypass** | Connector tools require `CONNECTED` state **and** the specific granted permission; EXTERNAL category requires confirmation by policy; every call is logged. |
| **Credential leakage** | Provider and connector secrets are AES-256-GCM encrypted at rest with `MASTER_KEY` (never stored in the DB; required in production), bound to owner via additional authenticated data, shown only as masked hints, never returned by the API. Logs pass through a redactor (key/token/password/cookie attributes and `sk-…`, `gsk_…`, `AIza…`, `ghp_…`, bearer patterns). Provider errors are redacted before reaching users or the model. `.env` is gitignored; CI runs gitleaks. |
| **SSRF (URL imports, web connector)** | http(s) only, ports 80/443, no embedded credentials, `localhost`/`.local`/`.internal` blocked, every resolved address must be public (loopback, private, link-local incl. `169.254.169.254`, CGNAT, ULA, documentation ranges blocked) — checked in the dialer so DNS rebinding cannot bypass it; redirects re-checked; size/time limits; environment proxies ignored. |
| **Malicious uploads / zip bombs / path traversal** | Size caps, in-memory ZIP processing with per-entry and total decompression limits, archives with `..`/absolute paths rejected, filenames reduced to a safe base name, nothing written to disk, NUL/invalid UTF-8 stripped. |
| **Rate-limit bypass** | Per-session (or per-IP for auth) fixed windows in Redis with in-memory fallback; `X-Forwarded-For` only the left-most entry is used. Agent, import and auth buckets are separate and configurable. |
| **History tampering** | Immutability enforced by PostgreSQL triggers, not application convention. |
| **Chain-of-thought exposure** | Reasoning output disabled at the provider (Groq `include_reasoning:false`/`reasoning_format:hidden`), `<think>` blocks stripped, Anthropic thinking replayed opaquely only to the same model, never stored or shown. |

## Operational guidance

- Generate `MASTER_KEY` with `openssl rand -base64 32` and keep it in your host's secret store. Losing it makes stored provider/connector credentials unreadable (re-enter them); it does not affect your ideas.
- Set `COOKIE_SECURE=true` and `APP_ENV=production` behind HTTPS (the defaults in production).
- Keep `WEB_ORIGINS` to your exact frontend origin(s).
- Rotate any API key that has been pasted into chats or tickets.

## Reporting

This is a personal project; please open a private security advisory on the GitHub repository rather than a public issue.
