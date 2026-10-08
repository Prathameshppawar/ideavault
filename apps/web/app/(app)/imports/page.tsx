import type { Metadata } from "next";
import { Suspense } from "react";
import { ImportsScreen } from "@/features/imports/imports-screen";
import { Spinner } from "@/components/ui/primitives";

export const metadata: Metadata = { title: "Import Center" };

export default function ImportsPage() {
  return (
    <Suspense fallback={<div className="flex h-[60vh] items-center justify-center"><Spinner /></div>}>
      <ImportsScreen />
    </Suspense>
  );
}
