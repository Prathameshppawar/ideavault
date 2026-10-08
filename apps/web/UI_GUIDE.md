# IdeaVault UI guide

IdeaVault should feel like a calm, serious thinking tool — closer to a well-made editor or journal than an admin dashboard.

## Stack & conventions

- **Next.js 16 (App Router, Cache Components on), React 19, TypeScript strict, Tailwind v4.** Read `node_modules/next/dist/docs/` when unsure — APIs differ from older Next versions (e.g. middleware is `proxy.ts`).
- **Pages are client-rendered screens.** A route's `page.tsx` is a tiny Server Component that exports `metadata` and renders `<Suspense fallback={…}><Screen /></Suspense>`; the screen lives in `features/<area>/…` with `"use client"`. Use `useParams()` / `useSearchParams()` inside the screen (they must sit under `<Suspense>` with Cache Components).
- **Data:** TanStack Query + `api` from `@/lib/api` (same-origin `/api/*`, CSRF header added automatically). Query keys: `["idea", id]`, `["ideas", …filters]`, `["artifact", id]`, etc. Invalidate after mutations. Show `Skeleton*` while loading, `ErrorState` with retry on error, `EmptyState` when empty.
- **Types:** `@/lib/types` (aliases of `@/types/api.generated`, generated from `packages/contracts/openapi.json`). Never hand-write API response types that already exist there.
- **Mutations:** `useMutation` + `toast` from `sonner` for success/failure (`errorMessage(err)`).
- **Links to entities:** `EntityChip` / `entityHref()` from `@/lib/entities`. Markdown (chat, artifacts) → `<Markdown>` (safe; `iv://type/id` links become chips; never use `dangerouslySetInnerHTML` for user/LLM content).

## Visual language

- Warm paper / deep ink neutrals, one accent (`accent`, ember). Each entity kind has a hue: `k-decision`, `k-assumption`, `k-evidence`, `k-insight`, `k-question`, `k-action`, `k-checkpoint`, `k-branch`, `k-artifact`, `k-conversation`, `k-idea` (+ `-soft` backgrounds). Use them for meaning, not decoration.
- Typography: `font-display` (Instrument Serif) for page titles and idea titles; Geist for UI. Section labels: `SectionTitle` (small uppercase, muted). Body 14–15px.
- Structure with whitespace and hairlines (`border-border`), not stacks of shadowed cards. `Panel` is a quiet bordered surface — use sparingly. Radius: 4–10px. **No** gradients, glassmorphism, emoji, stat-card grids, lorem ipsum or fake data.
- Layout: content max-width ~`max-w-5xl`/`max-w-6xl`, page padding `px-8 py-10`. `PageHeader` for page titles.
- Motion: subtle (`animate-fade-in`, `animate-slide-up`), no bouncing.
- Every interactive element is keyboard reachable with visible focus; icons have `aria-label`s; dialogs via `@/components/ui/overlay`.
- Dark mode must work (all colours come from tokens — never hard-code hex colours in components; for charts read CSS variables e.g. `var(--k-decision)`).

## Building blocks (import, don't re-implement)

- `@/components/ui/button` — `Button` (`primary | secondary | ghost | outline | danger | subtle`, sizes `xs|sm|md|lg|icon|icon-sm`, `loading`).
- `@/components/ui/primitives` — `Input`, `Textarea`, `Field`, `Label`, `Panel`, `SectionTitle`, `PageHeader`, `Skeleton`, `SkeletonLines`, `Spinner`, `EmptyState`, `ErrorState`, `Kbd`, `Divider`, `Meta`.
- `@/components/ui/overlay` — `Dialog/DialogTrigger/DialogContent/DialogClose`, `SheetContent` (right drawer), `Dropdown*`, `Popover*`, `Tooltip`, `Tabs/TabsList/TabsTrigger/TabsContent`, `Switch`, `Checkbox`.
- `@/components/ui/badges` — `Badge`, `StatusBadge` (idea lifecycle), `ItemStatus`, `EntityChip`, `KindTag`, `OriginTag` (SOURCE vs INTERPRETATION — always show it next to knowledge).
- `@/components/domain/knowledge` — `RefLabel`, `KnowledgeRow`, `IdeaRow`.
- `@/components/markdown` — `Markdown`.
- `@/features/chat/chat-view` — `ChatView` (pass `ideaId`, `branchId`, `checkpointId`, `compact`) for embedded chat.
- `@/lib/format` — `cn`, `timeAgo`, `shortDate`, `fullDate`, `humanize`, `plural`, `usd`, `compactNumber`, `truncate`.
- `@/lib/api` — `api.get/post/put/patch/del/upload`, `downloadFromApi(path)`, `downloadText(name, text)`, `errorMessage`.

## Product rules to respect in the UI

- History is never rewritten: superseded/reversed items stay visible (struck-through) and link to what replaced them.
- Always distinguish **Source** (what was actually said) from **Interpretation** (what IdeaVault inferred).
- Imported/external content is untrusted: label it as such.
- Destructive actions (delete idea/artifact) need an explicit confirmation dialog and send `X-Confirm: delete`.
- Proposed knowledge (from imports/extraction) needs review (accept/reject) before it counts.
