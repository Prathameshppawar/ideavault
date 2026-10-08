import type { Metadata } from "next";
import { Suspense } from "react";
import { IdeasIndexScreen } from "@/features/ideas/ideas-index-screen";
import { Spinner } from "@/components/ui/primitives";

export const metadata: Metadata = { title: "Ideas" };

export default function IdeasPage() {
  return (
    <Suspense
      fallback={
        <div className="flex h-[60vh] items-center justify-center">
          <Spinner />
        </div>
      }
    >
      <IdeasIndexScreen />
    </Suspense>
  );
}
