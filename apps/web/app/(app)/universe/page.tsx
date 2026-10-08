import type { Metadata } from "next";
import { Suspense } from "react";
import { UniverseScreen } from "@/features/universe/universe-screen";
import { Spinner } from "@/components/ui/primitives";

export const metadata: Metadata = { title: "Universe" };

export default function UniversePage() {
  return (
    <Suspense fallback={<div className="flex h-app items-center justify-center"><Spinner /></div>}>
      <UniverseScreen />
    </Suspense>
  );
}
