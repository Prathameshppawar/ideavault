import type { Metadata } from "next";
import { Suspense } from "react";
import { SettingsScreen } from "@/features/settings/settings-screen";
import { PlatformFallback } from "@/features/usage/fallback";

export const metadata: Metadata = { title: "Settings" };

export default function SettingsPage() {
  return (
    <Suspense fallback={<PlatformFallback />}>
      <SettingsScreen />
    </Suspense>
  );
}
