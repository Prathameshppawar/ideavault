import type { Metadata } from "next";
import { Suspense } from "react";
import { IdeaScreen } from "@/features/ideas/idea-screen";
import { Spinner } from "@/components/ui/primitives";

export const metadata: Metadata = { title: "Idea" };

export default function IdeaPage() {
  return (
    <Suspense
      fallback={
        <div className="flex h-app items-center justify-center">
          <Spinner />
        </div>
      }
    >
      <IdeaScreen />
    </Suspense>
  );
}
