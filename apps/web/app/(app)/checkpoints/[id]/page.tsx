import type { Metadata } from "next";
import { Suspense } from "react";
import { CheckpointScreen } from "@/features/checkpoints/checkpoint-screen";
import { Spinner } from "@/components/ui/primitives";

export const metadata: Metadata = { title: "Checkpoint" };

export default function CheckpointPage() {
  return (
    <Suspense
      fallback={
        <div className="flex h-[60vh] items-center justify-center">
          <Spinner />
        </div>
      }
    >
      <CheckpointScreen />
    </Suspense>
  );
}
