"use client";

import { create } from "zustand";
import { persist } from "zustand/middleware";

type ThemeMode = "light" | "dark" | "system";

interface ThemeState {
  mode: ThemeMode;
  resolved: "light" | "dark";
  setMode: (m: ThemeMode) => void;
  sync: () => void;
}

function systemDark() {
  return typeof window !== "undefined" && window.matchMedia("(prefers-color-scheme: dark)").matches;
}

function apply(resolved: "light" | "dark") {
  if (typeof document === "undefined") return;
  document.documentElement.classList.toggle("dark", resolved === "dark");
}

export const useTheme = create<ThemeState>()(
  persist(
    (set, get) => ({
      mode: "system",
      resolved: "light",
      setMode: (mode) => {
        const resolved = mode === "system" ? (systemDark() ? "dark" : "light") : mode;
        apply(resolved);
        set({ mode, resolved });
      },
      sync: () => get().setMode(get().mode),
    }),
    { name: "iv-theme", partialize: (s) => ({ mode: s.mode }) },
  ),
);

interface UIState {
  sidebarCollapsed: boolean;
  toggleSidebar: () => void;
  paletteOpen: boolean;
  setPaletteOpen: (v: boolean) => void;
  /** Chat panel on idea pages. */
  ideaChatOpen: boolean;
  setIdeaChatOpen: (v: boolean) => void;
}

export const useUI = create<UIState>()(
  persist(
    (set) => ({
      sidebarCollapsed: false,
      toggleSidebar: () => set((s) => ({ sidebarCollapsed: !s.sidebarCollapsed })),
      paletteOpen: false,
      setPaletteOpen: (paletteOpen) => set({ paletteOpen }),
      ideaChatOpen: true,
      setIdeaChatOpen: (ideaChatOpen) => set({ ideaChatOpen }),
    }),
    { name: "iv-ui", partialize: (s) => ({ sidebarCollapsed: s.sidebarCollapsed, ideaChatOpen: s.ideaChatOpen }) },
  ),
);
