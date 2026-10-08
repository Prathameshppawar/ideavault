import type { Metadata } from "next";
import { Suspense } from "react";
import { ArtifactsScreen } from "@/features/artifacts/artifacts-screen";
import { Spinner } from "@/components/ui/primitives";

export const metadata: Metadata = { title: "Artifacts" };

export default function ArtifactsPage() {
  return (
    <Suspense fallback={<div className="flex h-[60vh] items-center justify-center"><Spinner /></div>}>
      <ArtifactsScreen />
    </Suspense>
  );
}
