import type { Metadata } from "next";
import { Suspense } from "react";
import { BranchRedirect } from "@/features/ideas/branch-redirect";
import { Spinner } from "@/components/ui/primitives";

export const metadata: Metadata = { title: "Branch" };

export default function BranchPage() {
  return (
    <Suspense
      fallback={
        <div className="flex h-[60vh] items-center justify-center">
          <Spinner />
        </div>
      }
    >
      <BranchRedirect />
    </Suspense>
  );
}
