import type { Metadata } from "next";
import { Suspense } from "react";
import { ImportScreen } from "@/features/imports/import-screen";
import { Spinner } from "@/components/ui/primitives";

export const metadata: Metadata = { title: "Import" };

export default function ImportPage() {
  return (
    <Suspense fallback={<div className="flex h-[60vh] items-center justify-center"><Spinner /></div>}>
      <ImportScreen />
    </Suspense>
  );
}
