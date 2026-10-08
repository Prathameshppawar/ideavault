import type { Metadata } from "next";
import { Suspense } from "react";
import { ConnectorsScreen } from "@/features/connectors/connectors-screen";
import { PlatformFallback } from "@/features/usage/fallback";

export const metadata: Metadata = { title: "Connectors" };

export default function ConnectorsPage() {
  return (
    <Suspense fallback={<PlatformFallback />}>
      <ConnectorsScreen />
    </Suspense>
  );
}
