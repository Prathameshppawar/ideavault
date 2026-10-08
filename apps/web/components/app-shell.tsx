"use client";

import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import { useEffect, useState, type ReactNode } from "react";
import {
  Activity,
  BarChart3,
  Boxes,
  ChevronsLeft,
  FileText,
  Import,
  Lightbulb,
  LogOut,
  Menu,
  MessageSquare,
  Monitor,
  Moon,
  Orbit,
  Plug,
  Search,
  Settings,
  Sparkles,
  Sun,
  Telescope,
  X,
} from "lucide-react";
import { cn } from "@/lib/format";
import { useLogout, useSession, useSystemStatus } from "@/hooks/use-session";
import { useTheme, useUI } from "@/stores/ui";
import { Dropdown, DropdownContent, DropdownItem, DropdownLabel, DropdownSeparator, DropdownTrigger, Tooltip } from "@/components/ui/overlay";
import { Kbd, Spinner } from "@/components/ui/primitives";
import { CommandPalette } from "@/components/command-palette";

const primary = [
  { href: "/", label: "Chat", icon: MessageSquare, exact: true },
  { href: "/dashboard", label: "Thinking", icon: Activity },
  { href: "/ideas", label: "Ideas", icon: Lightbulb },
  { href: "/universe", label: "Universe", icon: Orbit },
  { href: "/search", label: "Search", icon: Search },
  { href: "/artifacts", label: "Artifacts", icon: FileText },
  { href: "/imports", label: "Import", icon: Import },
  { href: "/insights", label: "Insights", icon: Telescope },
];

const secondary = [
  { href: "/models", label: "Models", icon: Boxes },
  { href: "/usage", label: "Usage", icon: BarChart3 },
  { href: "/connectors", label: "Connectors", icon: Plug },
  { href: "/settings", label: "Settings", icon: Settings },
];

function NavItem({ href, label, icon: Icon, active, collapsed }: { href: string; label: string; icon: React.ComponentType<{ className?: string }>; active: boolean; collapsed: boolean }) {
  const link = (
    <Link
      href={href}
      className={cn(
        "group flex h-8 items-center gap-2.5 rounded-[var(--radius-md)] px-2.5 text-[13px] font-medium transition-colors",
        active ? "bg-surface-3 text-fg" : "text-muted hover:bg-surface-2 hover:text-fg",
        collapsed && "justify-center px-0",
      )}
      aria-current={active ? "page" : undefined}
    >
      <Icon className={cn("h-4 w-4 shrink-0", active ? "text-fg" : "text-faint group-hover:text-muted")} />
      {!collapsed && <span>{label}</span>}
    </Link>
  );
  return collapsed ? (
    <Tooltip content={label} side="right">
      {link}
    </Tooltip>
  ) : (
    link
  );
}

function ThemeMenuItems() {
  const { mode, setMode } = useTheme();
  const opts = [
    { m: "light" as const, label: "Light", icon: Sun },
    { m: "dark" as const, label: "Dark", icon: Moon },
    { m: "system" as const, label: "System", icon: Monitor },
  ];
  return (
    <>
      <DropdownLabel>Theme</DropdownLabel>
      {opts.map(({ m, label, icon: Icon }) => (
        <DropdownItem key={m} onSelect={() => setMode(m)}>
          <Icon className="h-3.5 w-3.5 text-muted" /> {label}
          {mode === m && <span className="ml-auto text-[11px] text-faint">current</span>}
        </DropdownItem>
      ))}
    </>
  );
}

export function AppShell({ children }: { children: ReactNode }) {
  const pathname = usePathname();
  const router = useRouter();
  const { data: session, isLoading } = useSession();
  const { data: status } = useSystemStatus();
  const { sidebarCollapsed: collapsed, toggleSidebar, setPaletteOpen } = useUI();
  const syncTheme = useTheme((s) => s.sync);
  const logout = useLogout();
  // Phones: the sidebar is an off-canvas drawer, closed again on every navigation.
  const [drawer, setDrawer] = useState(false);
  const [drawerPath, setDrawerPath] = useState(pathname);
  if (pathname !== drawerPath) {
    setDrawerPath(pathname);
    setDrawer(false);
  }
  // In the drawer the full labels always show; the collapsed rail is a desktop preference.
  const rail = collapsed && !drawer;

  useEffect(() => {
    if (!drawer) return;
    const onKey = (e: KeyboardEvent) => e.key === "Escape" && setDrawer(false);
    const desktop = window.matchMedia("(min-width: 768px)");
    const onResize = () => desktop.matches && setDrawer(false);
    window.addEventListener("keydown", onKey);
    desktop.addEventListener("change", onResize);
    return () => {
      window.removeEventListener("keydown", onKey);
      desktop.removeEventListener("change", onResize);
    };
  }, [drawer]);

  useEffect(() => syncTheme(), [syncTheme]);

  useEffect(() => {
    if (!isLoading && session && !session.authenticated) {
      router.replace(session.setup_required ? "/setup" : `/login?next=${encodeURIComponent(pathname)}`);
    }
  }, [isLoading, session, router, pathname]);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "k") {
        e.preventDefault();
        setPaletteOpen(true);
      }
      if ((e.metaKey || e.ctrlKey) && e.key === "\\") {
        e.preventDefault();
        toggleSidebar();
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [setPaletteOpen, toggleSidebar]);

  // Children render immediately (their own queries handle loading); we only bail out
  // once the session is known to be invalid, while the redirect is in flight.
  if (!isLoading && session && !session.authenticated) {
    return (
      <div className="flex h-full items-center justify-center">
        <Spinner />
      </div>
    );
  }

  const isActive = (href: string, exact?: boolean) => (exact ? pathname === href : pathname === href || pathname.startsWith(href + "/"));

  return (
    <div className="flex h-full flex-col md:flex-row">
      <header className="sticky top-0 z-30 flex h-12 shrink-0 items-center gap-1 border-b border-border bg-bg px-2 md:hidden" inert={drawer || undefined}>
        <button onClick={() => setDrawer(true)} className="rounded p-2 text-muted hover:bg-surface-2 hover:text-fg" aria-label="Open navigation" aria-expanded={drawer} aria-controls="app-nav">
          <Menu className="h-5 w-5" />
        </button>
        <Link href="/" className="flex items-center gap-2" aria-label="IdeaVault home">
          <span className="flex h-6 w-6 items-center justify-center rounded-[6px] bg-fg text-bg">
            <Sparkles className="h-3.5 w-3.5" />
          </span>
          <span className="font-display text-[1.2rem] leading-none text-fg">IdeaVault</span>
        </Link>
        <button onClick={() => setPaletteOpen(true)} className="ml-auto rounded p-2 text-muted hover:bg-surface-2 hover:text-fg" aria-label="Open command palette">
          <Search className="h-5 w-5" />
        </button>
      </header>
      {drawer && <div className="fixed inset-0 z-40 bg-black/30 animate-fade-in md:hidden" onClick={() => setDrawer(false)} aria-hidden />}
      <aside
        id="app-nav"
        className={cn(
          "flex flex-col border-r border-border bg-bg",
          "fixed inset-y-0 left-0 z-50 w-[264px] transition-transform duration-200",
          drawer ? "translate-x-0 shadow-lg" : "-translate-x-full",
          "md:sticky md:top-0 md:z-auto md:h-screen md:shrink-0 md:translate-x-0 md:shadow-none md:transition-[width]",
          collapsed ? "md:w-[60px]" : "md:w-[232px]",
        )}
        aria-label="Primary"
      >
        <div className={cn("flex h-14 items-center gap-2 px-4", rail && "justify-center px-0")}>
          <Link href="/" className="flex items-center gap-2" aria-label="IdeaVault home">
            <span className="flex h-7 w-7 items-center justify-center rounded-[7px] bg-fg text-bg">
              <Sparkles className="h-4 w-4" />
            </span>
            {!rail && <span className="font-display text-[1.35rem] leading-none text-fg">IdeaVault</span>}
          </Link>
        </div>
        <button
          onClick={() => setPaletteOpen(true)}
          className={cn(
            "mx-3 mb-3 flex h-8 items-center gap-2 rounded-[var(--radius-md)] border border-border bg-surface px-2.5 text-[13px] text-faint transition-colors hover:border-border-strong hover:text-muted",
            rail && "mx-auto w-9 justify-center px-0",
          )}
          aria-label="Open command palette"
        >
          <Search className="h-3.5 w-3.5" />
          {!rail && (
            <>
              <span>Ask or jump…</span>
              <Kbd className="ml-auto">⌘K</Kbd>
            </>
          )}
        </button>
        <nav className="flex-1 space-y-0.5 overflow-y-auto px-2.5">
          {primary.map((n) => (
            <NavItem key={n.href} {...n} active={isActive(n.href, n.exact)} collapsed={rail} />
          ))}
          <div className="py-3">
            <div className="h-px bg-border" />
          </div>
          {secondary.map((n) => (
            <NavItem key={n.href} {...n} active={isActive(n.href)} collapsed={rail} />
          ))}
        </nav>
        <div className={cn("border-t border-border p-2.5", rail && "flex flex-col items-center")}>
          {status?.offline_mode && !rail && (
            <Link href="/models" className="mb-2 block rounded-[var(--radius-md)] border border-warning/30 bg-warning-soft px-2.5 py-2 text-[12px] leading-snug text-warning">
              Offline mode — no AI provider. <span className="underline">Add one</span>
            </Link>
          )}
          <div className={cn("flex items-center gap-1", rail && "flex-col")}>
            <Dropdown>
              <DropdownTrigger asChild>
                <button className={cn("flex min-w-0 flex-1 items-center gap-2 rounded-[var(--radius-md)] px-1.5 py-1.5 text-left hover:bg-surface-2", rail && "flex-none")}>
                  <span className="flex h-6 w-6 shrink-0 items-center justify-center rounded-full bg-accent-soft text-[11px] font-semibold uppercase text-accent">
                    {(session?.user?.display_name || session?.user?.email || "·").slice(0, 1)}
                  </span>
                  {!rail && <span className="truncate text-[13px] text-fg">{session?.user?.display_name || session?.user?.email || " "}</span>}
                </button>
              </DropdownTrigger>
              <DropdownContent align="start" className="w-56">
                <DropdownLabel>{session?.user?.email}</DropdownLabel>
                <DropdownSeparator />
                <ThemeMenuItems />
                <DropdownSeparator />
                <DropdownItem onSelect={() => router.push("/settings")}>
                  <Settings className="h-3.5 w-3.5 text-muted" /> Settings
                </DropdownItem>
                <DropdownItem onSelect={() => void logout()}>
                  <LogOut className="h-3.5 w-3.5 text-muted" /> Sign out
                </DropdownItem>
              </DropdownContent>
            </Dropdown>
            {drawer ? (
              <button onClick={() => setDrawer(false)} className="rounded p-1.5 text-faint hover:bg-surface-2 hover:text-fg" aria-label="Close navigation">
                <X className="h-4 w-4" />
              </button>
            ) : (
              <Tooltip content={collapsed ? "Expand sidebar (⌘\\)" : "Collapse sidebar (⌘\\)"} side="right">
                <button onClick={toggleSidebar} className="hidden rounded p-1.5 text-faint hover:bg-surface-2 hover:text-fg md:block" aria-label="Toggle sidebar">
                  <ChevronsLeft className={cn("h-4 w-4 transition-transform", collapsed && "rotate-180")} />
                </button>
              </Tooltip>
            )}
          </div>
        </div>
      </aside>
      <main className="min-w-0 flex-1" inert={drawer || undefined}>
        {children}
      </main>
      <CommandPalette />
    </div>
  );
}
