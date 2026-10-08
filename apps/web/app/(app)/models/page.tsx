import type { Metadata } from "next";
import { Suspense } from "react";
import { ModelsScreen } from "@/features/models/models-screen";
import { PlatformFallback } from "@/features/usage/fallback";

export const metadata: Metadata = { title: "Models" };

export default function ModelsPage() {
  return (
    <Suspense fallback={<PlatformFallback />}>
      <ModelsScreen />
    </Suspense>
  );
}
