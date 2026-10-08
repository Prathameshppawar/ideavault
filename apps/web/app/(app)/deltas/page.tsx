import type { Metadata } from "next";
import { Suspense } from "react";
import { DeltasScreen } from "@/features/deltas/deltas-screen";
import { Spinner } from "@/components/ui/primitives";

export const metadata: Metadata = { title: "External conversations" };

export default function DeltasPage() {
  return (
    <Suspense fallback={<div className="flex h-[60vh] items-center justify-center"><Spinner /></div>}>
      <DeltasScreen />
    </Suspense>
  );
}
