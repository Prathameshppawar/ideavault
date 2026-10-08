import type { Metadata } from "next";
import { Suspense } from "react";
import { NewDeltaScreen } from "@/features/deltas/new-delta-screen";
import { Spinner } from "@/components/ui/primitives";

export const metadata: Metadata = { title: "Bring back a conversation" };

export default function NewDeltaPage() {
  return (
    <Suspense fallback={<div className="flex h-[60vh] items-center justify-center"><Spinner /></div>}>
      <NewDeltaScreen />
    </Suspense>
  );
}
