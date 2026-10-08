import type { Metadata } from "next";
import { Suspense } from "react";
import { InsightsScreen } from "@/features/insights/insights-screen";
import { PlatformFallback } from "@/features/usage/fallback";

export const metadata: Metadata = { title: "Insights" };

export default function InsightsPage() {
  return (
    <Suspense fallback={<PlatformFallback />}>
      <InsightsScreen />
    </Suspense>
  );
}
