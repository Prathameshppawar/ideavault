import type { Metadata } from "next";
import { Suspense } from "react";
import { KnowledgeScreen } from "@/features/knowledge/knowledge-screen";
import { Spinner } from "@/components/ui/primitives";

export const metadata: Metadata = { title: "Knowledge" };

export default function KnowledgePage() {
  return (
    <Suspense
      fallback={
        <div className="flex h-[60vh] items-center justify-center">
          <Spinner />
        </div>
      }
    >
      <KnowledgeScreen />
    </Suspense>
  );
}
