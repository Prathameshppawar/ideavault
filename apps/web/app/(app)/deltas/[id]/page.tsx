import type { Metadata } from "next";
import { Suspense } from "react";
import { DeltaScreen } from "@/features/deltas/delta-screen";
import { Spinner } from "@/components/ui/primitives";

export const metadata: Metadata = { title: "Review changes" };

export default function DeltaPage() {
  return (
    <Suspense fallback={<div className="flex h-[60vh] items-center justify-center"><Spinner /></div>}>
      <DeltaScreen />
    </Suspense>
  );
}
