import type { Metadata } from "next";
import { Suspense } from "react";
import { ContextPacksScreen } from "@/features/context-packs/context-packs-screen";
import { Spinner } from "@/components/ui/primitives";

export const metadata: Metadata = { title: "Context packs" };

export default function ContextPacksPage() {
  return (
    <Suspense fallback={<div className="flex h-[60vh] items-center justify-center"><Spinner /></div>}>
      <ContextPacksScreen />
    </Suspense>
  );
}
