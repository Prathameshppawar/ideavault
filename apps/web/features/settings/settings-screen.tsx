"use client";

import { useEffect, useState, useSyncExternalStore, type ReactNode } from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
import { BookOpen, Braces, Download, ExternalLink, Monitor, Moon, Sun } from "lucide-react";
import { toast } from "sonner";
import { api, errorMessage } from "@/lib/api";
import type { User } from "@/lib/types";
import { cn, fullDate } from "@/lib/format";
import { useTheme } from "@/stores/ui";
import { Button } from "@/components/ui/button";
import { ErrorState, Field, Input, PageHeader, SkeletonLines } from "@/components/ui/primitives";
import { KV, Segmented } from "@/features/usage/controls";
import { AgentSection } from "./agent-section";
import { SessionsSection } from "./sessions-section";
import { SystemSection } from "./system-section";

const SECTIONS = [
  { id: "profile", label: "Profile" },
  { id: "password", label: "Password" },
  { id: "sessions", label: "Sessions & API tokens" },
  { id: "agent", label: "Agent permissions" },
  { id: "appearance", label: "Appearance" },
  { id: "system", label: "System" },
  { id: "data", label: "Data & API" },
] as const;

export function SettingsScreen() {
  const active = useActiveSection(SECTIONS.map((s) => s.id));
  return (
    <div className="mx-auto max-w-6xl px-5 py-8 sm:px-8 sm:py-10">
      <PageHeader eyebrow="Platform" title="Settings" description="Your account, how much the agent may do on its own, and the state of this IdeaVault instance." />
      <div className="grid gap-10 lg:grid-cols-[200px_minmax(0,1fr)]">
        <nav aria-label="Settings sections" className="hidden lg:block">
          <ul className="sticky top-8 space-y-0.5">
            {SECTIONS.map((s) => (
              <li key={s.id}>
                <a
                  href={`#${s.id}`}
                  aria-current={active === s.id ? "true" : undefined}
                  className={cn(
                    "block rounded-[var(--radius-md)] px-2.5 py-1.5 text-[13px] transition-colors",
                    active === s.id ? "bg-surface-3 font-medium text-fg" : "text-muted hover:bg-surface-2 hover:text-fg",
                  )}
                >
                  {s.label}
                </a>
              </li>
            ))}
          </ul>
        </nav>
        <div className="min-w-0 max-w-3xl divide-y divide-border">
          <Section id="profile" title="Profile">
            <ProfileSection />
          </Section>
          <Section id="password" title="Password" description="At least 8 characters. Other signed-in sessions stay signed in — revoke them below if needed.">
            <PasswordSection />
          </Section>
          <Section id="sessions" title="Sessions & API tokens" description="Everywhere your vault is signed in, and the tokens scripts use to call the API.">
            <SessionsSection />
          </Section>
          <Section id="agent" title="Agent permissions" description="What the agent may do without asking. These rules are enforced by the server on every tool call — the model cannot change them.">
            <AgentSection />
          </Section>
          <Section id="appearance" title="Appearance">
            <AppearanceSection />
          </Section>
          <Section id="system" title="System">
            <SystemSection />
          </Section>
          <Section id="data" title="Data & API">
            <DataSection />
          </Section>
        </div>
      </div>
    </div>
  );
}

function Section({ id, title, description, children }: { id: string; title: string; description?: string; children: ReactNode }) {
  return (
    <section id={id} aria-labelledby={`${id}-title`} className="scroll-mt-8 py-9 first:pt-0">
      <h2 id={`${id}-title`} className="text-[17px] font-semibold text-fg">
        {title}
      </h2>
      {description && <p className="mt-1 max-w-2xl text-[13.5px] leading-relaxed text-muted">{description}</p>}
      <div className="mt-5">{children}</div>
    </section>
  );
}

/** Highlights the last section whose heading has scrolled past the top of the viewport. */
function useActiveSection(ids: readonly string[]) {
  const [active, setActive] = useState<string>(ids[0]);
  const key = ids.join(",");
  useEffect(() => {
    const list = key.split(",");
    let frame = 0;
    const update = () => {
      frame = 0;
      let current = list[0];
      for (const id of list) {
        const el = document.getElementById(id);
        if (el && el.getBoundingClientRect().top <= 140) current = id;
      }
      if (window.innerHeight + window.scrollY >= document.documentElement.scrollHeight - 4) current = list[list.length - 1];
      setActive(current);
    };
    const onScroll = () => {
      if (!frame) frame = requestAnimationFrame(update);
    };
    frame = requestAnimationFrame(update);
    window.addEventListener("scroll", onScroll, { passive: true });
    window.addEventListener("resize", onScroll);
    return () => {
      cancelAnimationFrame(frame);
      window.removeEventListener("scroll", onScroll);
      window.removeEventListener("resize", onScroll);
    };
  }, [key]);
  return active;
}

function ProfileSection() {
  const { data, isLoading, error, refetch } = useQuery({ queryKey: ["auth-me"], queryFn: () => api.get<User>("/v1/auth/me") });
  if (isLoading) return <SkeletonLines lines={3} />;
  if (error || !data) return <ErrorState error={error} onRetry={() => void refetch()} />;
  const initial = (data.display_name || data.email || "·").slice(0, 1).toUpperCase();
  return (
    <div className="flex flex-col gap-6 sm:flex-row sm:items-start">
      <span className="flex h-14 w-14 shrink-0 items-center justify-center rounded-full bg-accent-soft text-[1.35rem] font-semibold text-accent" aria-hidden>
        {initial}
      </span>
      <dl className="min-w-0 flex-1">
        <KV label="Name">{data.display_name || "—"}</KV>
        <KV label="Email">{data.email}</KV>
        <KV label="Owner since">{fullDate(data.created_at)}</KV>
        <KV label="User ID" mono>
          {data.id}
        </KV>
      </dl>
    </div>
  );
}

function PasswordSection() {
  const [current, setCurrent] = useState("");
  const [next, setNext] = useState("");
  const [confirm, setConfirm] = useState("");
  const [touched, setTouched] = useState(false);
  const change = useMutation({
    mutationFn: () => api.post("/v1/auth/password", { current, next }),
    onSuccess: () => {
      toast.success("Password changed");
      setCurrent("");
      setNext("");
      setConfirm("");
      setTouched(false);
    },
  });
  const tooShort = next.length > 0 && next.length < 8;
  const mismatch = confirm.length > 0 && confirm !== next;
  const same = next.length > 0 && next === current;
  const valid = current.length > 0 && next.length >= 8 && confirm === next && !same;
  return (
    <form
      className="max-w-md space-y-4"
      onSubmit={(e) => {
        e.preventDefault();
        setTouched(true);
        if (valid) change.mutate();
      }}
    >
      <Field label="Current password" htmlFor="pw-current">
        <Input id="pw-current" type="password" autoComplete="current-password" value={current} onChange={(e) => setCurrent(e.target.value)} required />
      </Field>
      <Field label="New password" htmlFor="pw-next" error={touched || tooShort ? (tooShort ? "Use at least 8 characters." : same ? "Choose a password different from the current one." : undefined) : undefined}>
        <Input id="pw-next" type="password" autoComplete="new-password" value={next} onChange={(e) => setNext(e.target.value)} required minLength={8} aria-invalid={tooShort || same || undefined} />
      </Field>
      <Field label="Confirm new password" htmlFor="pw-confirm" error={mismatch ? "Passwords don't match." : undefined}>
        <Input id="pw-confirm" type="password" autoComplete="new-password" value={confirm} onChange={(e) => setConfirm(e.target.value)} required aria-invalid={mismatch || undefined} />
      </Field>
      {change.error && (
        <p role="alert" className="rounded-[var(--radius-md)] border border-danger/30 bg-danger-soft px-3 py-2 text-[13px] text-danger">
          {errorMessage(change.error)}
        </p>
      )}
      <Button type="submit" variant="secondary" loading={change.isPending} disabled={!valid}>
        Change password
      </Button>
    </form>
  );
}

const noopSubscribe = () => () => {};

function AppearanceSection() {
  // The persisted theme is only known on the client; render the server default until hydrated.
  const hydrated = useSyncExternalStore(noopSubscribe, () => true, () => false);
  const stored = useTheme((s) => s.mode);
  const mode = hydrated ? stored : "system";
  const setMode = useTheme((s) => s.setMode);
  return (
    <div className="flex flex-wrap items-center justify-between gap-4">
      <div>
        <p className="text-[14px] text-fg">Theme</p>
        <p className="text-[12.5px] text-muted">System follows your operating system&apos;s light or dark setting.</p>
      </div>
      <Segmented
        label="Theme"
        value={mode}
        onChange={setMode}
        options={[
          { value: "light", label: "Light", icon: Sun },
          { value: "dark", label: "Dark", icon: Moon },
          { value: "system", label: "System", icon: Monitor },
        ]}
      />
    </div>
  );
}

function DataSection() {
  const links = [
    { href: "/api/docs", icon: BookOpen, title: "API reference", body: "Interactive OpenAPI docs for every endpoint." },
    { href: "/api/v1/openapi.json", icon: Braces, title: "openapi.json", body: "The machine-readable contract (OpenAPI 3)." },
  ];
  return (
    <div className="space-y-5">
      <ul className="grid gap-3 sm:grid-cols-2">
        {links.map(({ href, icon: Icon, title, body }) => (
          <li key={href}>
            <a
              href={href}
              target="_blank"
              rel="noopener noreferrer"
              className="group flex h-full items-start gap-3 rounded-[var(--radius-lg)] border border-border bg-surface px-4 py-3 transition-colors hover:border-border-strong"
            >
              <Icon className="mt-0.5 h-4 w-4 shrink-0 text-muted" />
              <span className="min-w-0 flex-1">
                <span className="flex items-center gap-1.5 text-[14px] text-fg">
                  {title} <ExternalLink className="h-3 w-3 text-faint group-hover:text-muted" aria-hidden />
                </span>
                <span className="block text-[12.5px] text-muted">{body}</span>
              </span>
            </a>
          </li>
        ))}
      </ul>
      <div className="flex items-start gap-3 text-[13px] leading-relaxed text-muted">
        <Download className="mt-0.5 h-4 w-4 shrink-0 text-faint" />
        <p>
          Your thinking stays portable: artifacts and context packs download as Markdown from their own pages, and everything else — ideas, branches, checkpoints, knowledge — is
          available through the API with a personal token.
        </p>
      </div>
    </div>
  );
}
