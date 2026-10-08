"use client";

import { useQueryClient } from "@tanstack/react-query";
import { useRouter, useSearchParams } from "next/navigation";
import { useEffect, useState, type FormEvent } from "react";
import { Sparkles } from "lucide-react";
import { api, errorMessage } from "@/lib/api";
import { useSession } from "@/hooks/use-session";
import { Button } from "@/components/ui/button";
import { Field, Input } from "@/components/ui/primitives";

export function AuthForm({ mode }: { mode: "login" | "setup" }) {
  const router = useRouter();
  const params = useSearchParams();
  const qc = useQueryClient();
  const { data: status } = useSession();
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [name, setName] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const next = params.get("next") || "/";
  const safeNext = next.startsWith("/") && !next.startsWith("//") ? next : "/";

  useEffect(() => {
    if (!status) return;
    if (status.authenticated) router.replace(safeNext);
    else if (mode === "login" && status.setup_required) router.replace("/setup");
    else if (mode === "setup" && !status.setup_required) router.replace("/login");
  }, [status, mode, router, safeNext]);

  async function submit(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError(null);
    try {
      await api.post(mode === "login" ? "/v1/auth/login" : "/v1/auth/setup", { email, password, display_name: name || undefined });
      await qc.invalidateQueries();
      router.replace(safeNext);
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="flex min-h-full items-center justify-center px-6 py-16">
      <div className="w-full max-w-sm animate-slide-up">
        <div className="mb-10 flex items-center gap-2.5">
          <span className="flex h-9 w-9 items-center justify-center rounded-[9px] bg-fg text-bg">
            <Sparkles className="h-5 w-5" />
          </span>
          <span className="font-display text-3xl text-fg">IdeaVault</span>
        </div>
        <h1 className="font-display text-[2.2rem] leading-tight text-fg">{mode === "login" ? "Welcome back." : "Create your vault."}</h1>
        <p className="mt-2 text-[15px] text-muted">
          {mode === "login"
            ? "Pick up your thinking exactly where you left it."
            : "This is a personal system: the first account becomes the vault owner. Everything you think here stays yours."}
        </p>
        <form onSubmit={submit} className="mt-8 space-y-4">
          {mode === "setup" && (
            <Field label="Your name" htmlFor="name">
              <Input id="name" value={name} onChange={(e) => setName(e.target.value)} placeholder="Prathamesh" autoComplete="name" />
            </Field>
          )}
          <Field label="Email" htmlFor="email">
            <Input id="email" type="email" required value={email} onChange={(e) => setEmail(e.target.value)} autoComplete="email" autoFocus />
          </Field>
          <Field label="Password" htmlFor="password" hint={mode === "setup" ? "At least 8 characters. Stored as an Argon2id hash." : undefined}>
            <Input
              id="password"
              type="password"
              required
              minLength={8}
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              autoComplete={mode === "login" ? "current-password" : "new-password"}
            />
          </Field>
          {error && (
            <p className="rounded-[var(--radius-md)] bg-danger-soft px-3 py-2 text-[13px] text-danger" role="alert">
              {error}
            </p>
          )}
          <Button type="submit" variant="primary" size="lg" className="w-full" loading={busy}>
            {mode === "login" ? "Sign in" : "Create vault"}
          </Button>
        </form>
      </div>
    </div>
  );
}
