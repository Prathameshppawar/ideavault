import type { Metadata } from "next";
import { Suspense } from "react";
import { UsageScreen } from "@/features/usage/usage-screen";
import { PlatformFallback } from "@/features/usage/fallback";

export const metadata: Metadata = { title: "Usage" };

export default function UsagePage() {
  return (
    <Suspense fallback={<PlatformFallback />}>
      <UsageScreen />
    </Suspense>
  );
}
