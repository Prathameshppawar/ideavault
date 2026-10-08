import type { Metadata } from "next";
import { Suspense } from "react";
import { DashboardScreen } from "@/features/dashboard/dashboard-screen";
import { Spinner } from "@/components/ui/primitives";

export const metadata: Metadata = { title: "Thinking" };

export default function DashboardPage() {
  return (
    <Suspense fallback={<div className="flex h-app items-center justify-center"><Spinner /></div>}>
      <DashboardScreen />
    </Suspense>
  );
}
