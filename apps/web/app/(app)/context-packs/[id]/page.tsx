import type { Metadata } from "next";
import { Suspense } from "react";
import { ContextPackScreen } from "@/features/context-packs/context-pack-screen";
import { Spinner } from "@/components/ui/primitives";

export const metadata: Metadata = { title: "Context pack" };

export default function ContextPackPage() {
  return (
    <Suspense fallback={<div className="flex h-[60vh] items-center justify-center"><Spinner /></div>}>
      <ContextPackScreen />
    </Suspense>
  );
}
