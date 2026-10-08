import type { Metadata } from "next";
import { Suspense } from "react";
import { ArtifactScreen } from "@/features/artifacts/artifact-screen";
import { Spinner } from "@/components/ui/primitives";

export const metadata: Metadata = { title: "Artifact" };

export default function ArtifactPage() {
  return (
    <Suspense fallback={<div className="flex h-[60vh] items-center justify-center"><Spinner /></div>}>
      <ArtifactScreen />
    </Suspense>
  );
}
